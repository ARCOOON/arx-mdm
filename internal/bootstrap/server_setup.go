package bootstrap

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/mail"
	"os"
	"path/filepath"
	"strings"

	"github.com/ARCOOON/arx-mdm/internal/auth"
	"github.com/ARCOOON/arx-mdm/internal/database"
	"github.com/ARCOOON/arx-mdm/internal/pki"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/term"
)

const envServerDomain = "ARX_SERVER_DOMAIN"

// RunServerSetup initializes embedded PKI, mints the server TLS credential pair when needed,
// runs migrations, prompts for the initial admin email and password when the users table is empty,
// and prints recommended mTLS-related .env lines to out.
//
// Dependencies: envfile must already be loaded if used; caller must set ARX_PKI_STORAGE_PATH /
// defaults and ARX_DATABASE_URL consistent with deployment.
func RunServerSetup(ctx context.Context, log *slog.Logger, stdin io.Reader, out, errOut io.Writer, pkiDir, dsn string, connectPool func(string) (*pgxpool.Pool, error)) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	pkiDir = strings.TrimSpace(pkiDir)
	if pkiDir == "" {
		return fmt.Errorf("pki storage path is empty")
	}

	authority, err := pki.LoadOrInitialize(pkiDir)
	if err != nil {
		return fmt.Errorf("embedded pki: %w", err)
	}

	extraDNS := pki.NormalizeExtraTLSDNS(os.Getenv(envServerDomain))

	certAbs, keyAbs, minted, err := authority.EnsureServerTLSMaterial(ctx, extraDNS)
	if err != nil {
		return fmt.Errorf("server tls material: %w", err)
	}
	if minted {
		fmt.Fprintf(errOut, "Created server TLS certificate and key under %s\n", filepath.Clean(authority.StorageDir()))
	} else {
		fmt.Fprintf(errOut, "Server TLS PEM files already present under %s; left unchanged.\n", filepath.Clean(authority.StorageDir()))
	}

	pool, err := connectPool(dsn)
	if err != nil {
		return fmt.Errorf("database connection: %w", err)
	}
	defer pool.Close()

	if err := database.Migrate(ctx, pool, log); err != nil {
		return fmt.Errorf("database migrations: %w", err)
	}

	if err := ensureInitialAdminInteractive(ctx, pool, stdin, errOut); err != nil {
		return err
	}

	bundleAbs := filepath.Clean(authority.MTLSClientCABundlePath())
	if absBundle, absErr := filepath.Abs(bundleAbs); absErr == nil {
		bundleAbs = absBundle
	}

	pkiAbs := filepath.Clean(authority.StorageDir())
	if pa, paErr := filepath.Abs(pkiAbs); paErr == nil {
		pkiAbs = pa
	}

	printMTLSenvLines(out, certAbs, keyAbs, bundleAbs, pkiAbs)
	fmt.Fprintln(out, "Setup completed.")
	return nil
}

func ensureInitialAdminInteractive(ctx context.Context, pool *pgxpool.Pool, stdin io.Reader, errOut io.Writer) error {
	n, err := auth.CountUsers(ctx, pool)
	if err != nil {
		return fmt.Errorf("count users: %w", err)
	}
	if n > 0 {
		fmt.Fprintf(errOut, "Users already exist (%d rows); skipping initial admin bootstrap.\n", n)
		return nil
	}

	fmt.Fprint(errOut, "No dashboard users yet. Create the initial admin.\nEmail (stored as username): ")
	reader := bufio.NewReader(stdin)
	emailLine, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("read email: %w", err)
	}
	email, err := normalizeAdminEmail(emailLine)
	if err != nil {
		return err
	}

	var taken bool
	err = pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE lower(username) = lower($1))`, email).Scan(&taken)
	if err != nil {
		return fmt.Errorf("check username: %w", err)
	}
	if taken {
		return fmt.Errorf("user with email %q already exists", email)
	}

	pass, err := readPassword_ttySafe(errOut, reader, "Password: ")
	if err != nil {
		return fmt.Errorf("read password: %w", err)
	}
	pass2, err := readPassword_ttySafe(errOut, reader, "Confirm password: ")
	if err != nil {
		return fmt.Errorf("read password confirmation: %w", err)
	}
	if pass != pass2 {
		return fmt.Errorf("passwords do not match")
	}

	hash, err := auth.HashPassword(pass)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	_, err = pool.Exec(ctx, `
INSERT INTO users (username, password_hash, role) VALUES ($1, $2, $3)
`, email, hash, auth.RoleAdmin)
	if err != nil {
		return fmt.Errorf("insert admin user: %w", err)
	}
	fmt.Fprintf(errOut, "Initial admin created (username is the email).\n")
	return nil
}

func normalizeAdminEmail(raw string) (string, error) {
	addr, err := mail.ParseAddress(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("invalid email address: %w", err)
	}
	if addr.Address == "" {
		return "", fmt.Errorf("invalid email address: empty mailbox")
	}
	return strings.ToLower(addr.Address), nil
}

func readPassword_ttySafe(promptTo io.Writer, reader *bufio.Reader, prompt string) (string, error) {
	fmt.Fprint(promptTo, prompt)
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		b, err := term.ReadPassword(fd)
		fmt.Fprintln(promptTo)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func printMTLSenvLines(out io.Writer, tlsCertAbs, tlsKeyAbs, mtlsBundleAbs, pkiAbs string) {
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Append these lines to your .env for server mTLS:")
	fmt.Fprintf(out, "ARX_TLS_CERT=%s\n", tlsCertAbs)
	fmt.Fprintf(out, "ARX_TLS_KEY=%s\n", tlsKeyAbs)
	fmt.Fprintf(out, "ARX_MTLS_CLIENT_CA_BUNDLE=%s\n", mtlsBundleAbs)
	fmt.Fprintf(out, "%s=%s\n", "ARX_PKI_STORAGE_PATH", pkiAbs)
	fmt.Fprintln(out, "")
}
