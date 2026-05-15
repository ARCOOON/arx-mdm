package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ARCOOON/arx-mdm/internal/cli"
	"github.com/ARCOOON/arx-mdm/internal/database"
	"github.com/ARCOOON/arx-mdm/internal/envfile"
	"github.com/ARCOOON/arx-mdm/internal/pki"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"
)

const (
	envListenAddr           = "ARX_LISTEN_ADDR"
	envDatabaseURL          = "ARX_DATABASE_URL"
	envPKIStoragePath       = "ARX_PKI_STORAGE_PATH"
	envTLSCert              = "ARX_TLS_CERT"
	envTLSKey               = "ARX_TLS_KEY"
	envMTLSClientCABundle   = "ARX_MTLS_CLIENT_CA_BUNDLE"
	envJWTSecret            = "ARX_JWT_SECRET"
	envJWTIssuer            = "ARX_JWT_ISSUER"
	envJWTTTL               = "ARX_JWT_TTL"
	envBootstrapAdminUser   = "ARX_BOOTSTRAP_ADMIN_USERNAME"
	envBootstrapAdminPass   = "ARX_BOOTSTRAP_ADMIN_PASSWORD"
)

func main() {
	if err := envfile.LoadFromCWD(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: load .env: %v\n", err)
	}
	ensureStandaloneDatabaseURL()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	root := newRootCmd(logger)
	if err := root.Execute(); err != nil {
		logger.Error("command failed", "err", err)
		os.Exit(1)
	}
}

func newRootCmd(logger *slog.Logger) *cobra.Command {
	root := &cobra.Command{
		Use:          filepath.Base(os.Args[0]),
		Short:        "ARX MDM server and operator tooling",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServe(logger)
		},
	}

	serveCmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the HTTP/WebSocket MDM server (default when no subcommand is given)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServe(logger)
		},
	}

	adminCmd := &cobra.Command{
		Use:   "admin",
		Short: "Database administration (requires ARX_DATABASE_URL)",
	}

	adminSetupCmd := &cobra.Command{
		Use:   "setup",
		Short: "If no admin users exist, create user 'admin' with a random password and print credentials",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withDBAndMigrations(cmd.Context(), logger, func(ctx context.Context, pool *pgxpool.Pool) error {
				return cli.RunSetup(ctx, pool, os.Stdout, os.Stderr)
			})
		},
	}

	adminCreateUserCmd := &cobra.Command{
		Use:   "create-user",
		Short: "Interactively create a dashboard user (username, password, role)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withDBAndMigrations(cmd.Context(), logger, func(ctx context.Context, pool *pgxpool.Pool) error {
				return cli.RunCreateUserInteractive(ctx, pool, os.Stderr)
			})
		},
	}

	adminCreateTokenCmd := &cobra.Command{
		Use:   "create-token",
		Short: "Generate a new enrollment token and print the presentation secret to stdout",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withDBAndMigrations(cmd.Context(), logger, func(ctx context.Context, pool *pgxpool.Pool) error {
				return cli.RunCreateEnrollmentToken(ctx, pool, os.Stdout, os.Stderr)
			})
		},
	}

	adminResetPasswordCmd := &cobra.Command{
		Use:   "reset-password USERNAME",
		Short: "Reset password for an existing user (prompts for new password)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withDBAndMigrations(cmd.Context(), logger, func(ctx context.Context, pool *pgxpool.Pool) error {
				return cli.RunResetPassword(ctx, pool, args[0], os.Stderr)
			})
		},
	}

	adminCmd.AddCommand(adminSetupCmd, adminCreateUserCmd, adminCreateTokenCmd, adminResetPasswordCmd)

	pkiBootstrapCmd := &cobra.Command{
		Use:   "bootstrap",
		Short: "Ensure embedded Root + Intermediate CA material exists under ARX_PKI_STORAGE_PATH",
		RunE: func(cmd *cobra.Command, args []string) error {
			pkiDir := strings.TrimSpace(os.Getenv(envPKIStoragePath))
			if pkiDir == "" {
				pkiDir = "certs"
			}
			a, err := pki.LoadOrInitialize(pkiDir)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(os.Stderr, "PKI ready storage=%s mtls_client_ca_bundle=%s\n", a.StorageDir(), a.MTLSClientCABundlePath())
			return nil
		},
	}

	pkiCmd := &cobra.Command{
		Use:   "pki",
		Short: "Public key infrastructure helpers",
	}
	pkiCmd.AddCommand(pkiBootstrapCmd)

	root.AddCommand(serveCmd, adminCmd, pkiCmd)
	return root
}

func withDBAndMigrations(ctx context.Context, logger *slog.Logger, fn func(context.Context, *pgxpool.Pool) error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	dsn := strings.TrimSpace(os.Getenv(envDatabaseURL))
	if dsn == "" {
		return fmt.Errorf("missing %s", envDatabaseURL)
	}
	pool, err := connectDatabasePool(dsn)
	if err != nil {
		return fmt.Errorf("database connection: %w", err)
	}
	defer pool.Close()

	migrateCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if err := database.Migrate(migrateCtx, pool, logger); err != nil {
		return fmt.Errorf("database migrations: %w", err)
	}

	opCtx, opCancel := context.WithTimeout(ctx, 30*time.Second)
	defer opCancel()
	return fn(opCtx, pool)
}

func connectDatabasePool(dsn string) (*pgxpool.Pool, error) {
	pgxCtx, pgxCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer pgxCancel()
	pool, err := pgxpool.New(pgxCtx, dsn)
	if err != nil {
		return nil, err
	}
	pingCtx, pingCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer pingCancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

func buildClientAuthTLSConfig(clientCABundlePath string) (*tls.Config, error) {
	pemData, err := os.ReadFile(clientCABundlePath)
	if err != nil {
		return nil, fmt.Errorf("read client ca bundle: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pemData) {
		return nil, fmt.Errorf("no client CA certificates parsed from %s", clientCABundlePath)
	}
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		ClientCAs:  pool,
		ClientAuth: tls.VerifyClientCertIfGiven,
	}, nil
}

func withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rid := r.Header.Get("X-Request-Id")
		if rid == "" {
			rid = uuid.NewString()
			r.Header.Set("X-Request-Id", rid)
		}
		w.Header().Set("X-Request-Id", rid)
		next.ServeHTTP(w, r)
	})
}

// ensureStandaloneDatabaseURL maps docker-compose style POSTGRES_* vars to ARX_DATABASE_URL when unset.
func ensureStandaloneDatabaseURL() {
	if strings.TrimSpace(os.Getenv(envDatabaseURL)) != "" {
		return
	}
	pass := strings.TrimSpace(os.Getenv("POSTGRES_PASSWORD"))
	if pass == "" {
		return
	}
	user := strings.TrimSpace(os.Getenv("POSTGRES_USER"))
	if user == "" {
		user = "arx"
	}
	db := strings.TrimSpace(os.Getenv("POSTGRES_DB"))
	if db == "" {
		db = "arx"
	}
	host := strings.TrimSpace(os.Getenv("POSTGRES_HOST"))
	if host == "" {
		host = "127.0.0.1"
	}
	port := strings.TrimSpace(os.Getenv("POSTGRES_PORT"))
	if port == "" {
		port = "5432"
	}
	sslmode := strings.TrimSpace(os.Getenv("POSTGRES_SSLMODE"))
	if sslmode == "" {
		sslmode = "disable"
	}
	dsn := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s", user, pass, host, port, db, sslmode)
	_ = os.Setenv(envDatabaseURL, dsn)
}

func parseDashboardOrigins(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []string{"http://localhost:5173", "http://127.0.0.1:5173"}
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return []string{"http://localhost:5173", "http://127.0.0.1:5173"}
	}
	return out
}
