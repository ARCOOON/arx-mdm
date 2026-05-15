package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"arx-mdm/internal/api"
	"arx-mdm/internal/auth"
	"arx-mdm/internal/database"
	"arx-mdm/internal/notifications"
	"arx-mdm/internal/pki"
	"arx-mdm/internal/scheduler"
	"arx-mdm/internal/serverinstall"
	"arx-mdm/internal/ws"

	"github.com/google/uuid"
)

func runServe(logger *slog.Logger) error {
	dsn := strings.TrimSpace(os.Getenv(envDatabaseURL))
	if dsn == "" {
		return fmt.Errorf("missing %s", envDatabaseURL)
	}

	listenAddr := os.Getenv(envListenAddr)
	if listenAddr == "" {
		listenAddr = ":8080"
	}

	pool, err := connectDatabasePool(dsn)
	if err != nil {
		return fmt.Errorf("database connection: %w", err)
	}
	defer pool.Close()

	migrateCtx, migrateCancel := context.WithTimeout(context.Background(), 2*time.Minute)
	if err := database.Migrate(migrateCtx, pool, logger); err != nil {
		migrateCancel()
		return fmt.Errorf("database migrations: %w", err)
	}
	migrateCancel()
	logger.Info("database migrations complete")

	pkiDir := strings.TrimSpace(os.Getenv(envPKIStoragePath))
	if pkiDir == "" {
		pkiDir = "certs"
	}
	caAuthority, err := pki.LoadOrInitialize(pkiDir)
	if err != nil {
		return fmt.Errorf("embedded pki initialization: %w", err)
	}
	logger.Info("embedded pki ready", "storage", caAuthority.StorageDir(), "mtls_client_ca_bundle", caAuthority.MTLSClientCABundlePath())

	store := auth.NewPGXEnrollmentStore(pool)
	coordinator := auth.NewEnrollmentCoordinator(store, caAuthority)

	tlsCertPath := strings.TrimSpace(os.Getenv(envTLSCert))
	tlsKeyPath := strings.TrimSpace(os.Getenv(envTLSKey))
	mtlsCAPath := strings.TrimSpace(os.Getenv(envMTLSClientCABundle))
	mtlsReady := tlsCertPath != "" && tlsKeyPath != "" && mtlsCAPath != ""

	var tlsCfg *tls.Config
	if mtlsReady {
		tlsCfg, err = buildClientAuthTLSConfig(mtlsCAPath)
		if err != nil {
			return fmt.Errorf("server tls configuration: %w", err)
		}
		logger.Info("server tls enabled for mutual client verification on telemetry",
			"client_ca_bundle", mtlsCAPath,
		)
	} else {
		logger.Warn("server tls not fully configured; telemetry will reject requests until ARX_TLS_CERT, ARX_TLS_KEY, and ARX_MTLS_CLIENT_CA_BUNDLE are set")
	}

	c2Hub := ws.NewHub()
	dashHub := ws.NewDashboardHub()
	dashboardOrigins := parseDashboardOrigins(os.Getenv("ARX_DASHBOARD_ORIGINS"))

	alerterCtx, alerterCancel := context.WithCancel(context.Background())
	defer alerterCancel()
	alerter := notifications.NewAlerter(pool, logger, notifications.Options{})
	alerter.Start(alerterCtx)
	go scheduler.Run(alerterCtx, scheduler.Deps{
		Pool:           pool,
		Hub:            c2Hub,
		Logger:         logger,
		ReloadInterval: time.Minute,
	})

	jwtSecret := strings.TrimSpace(os.Getenv(envJWTSecret))
	if jwtSecret == "" {
		return fmt.Errorf("missing %s", envJWTSecret)
	}
	jwtIssuer := strings.TrimSpace(os.Getenv(envJWTIssuer))
	jwtTTL := 24 * time.Hour
	if raw := strings.TrimSpace(os.Getenv(envJWTTTL)); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return fmt.Errorf("invalid %s: %w", envJWTTTL, err)
		}
		jwtTTL = d
	}
	jwtSvc, err := auth.NewJWTService(jwtSecret, jwtIssuer, jwtTTL)
	if err != nil {
		return fmt.Errorf("jwt service init: %w", err)
	}

	bootCtx, bootCancel := context.WithTimeout(context.Background(), 15*time.Second)
	bootErr := auth.BootstrapAdminIfEmpty(
		bootCtx,
		pool,
		os.Getenv(envBootstrapAdminUser),
		os.Getenv(envBootstrapAdminPass),
	)
	bootCancel()
	if bootErr != nil {
		return fmt.Errorf("bootstrap admin: %w", bootErr)
	}

	dashAuth := api.DashboardAuth{JWT: jwtSvc, Origins: dashboardOrigins}

	mux := http.NewServeMux()
	api.RegisterPublicEnrollRoute(mux, api.EnrollPublicDeps{
		Logger:      logger,
		Coordinator: coordinator,
	})
	mux.HandleFunc("POST /v1/telemetry", api.NewTelemetryHandler(api.TelemetryDeps{
		Pool:            pool,
		Logger:          logger,
		MTLSRequired:    mtlsReady,
		AdvisoryLockKey: api.AdvisoryLockKeyARXClientSeq,
		OnHeartbeat: func(ctx context.Context, assetID uuid.UUID) {
			alerter.ClearStaleAck(ctx, assetID)
		},
		OnTelemetryAccepted: func(certSerial, humanID string, assetID uuid.UUID, payload api.TelemetryPayload) {
			msg, err := ws.MarshalTelemetryUpdate(c2Hub, certSerial, humanID, assetID, payload)
			if err != nil {
				return
			}
			dashHub.Broadcast(msg)
		},
	}))

	api.RegisterAuthRoutes(mux, api.AuthDeps{
		Pool:    pool,
		Logger:  logger,
		JWT:     jwtSvc,
		Origins: dashboardOrigins,
	})
	api.RegisterAuditRoutes(mux, api.AuditDeps{Pool: pool, Logger: logger, Auth: dashAuth})
	api.RegisterUsersAdminRoutes(mux, api.UsersAdminDeps{Pool: pool, Logger: logger, Auth: dashAuth})
	api.RegisterKnowledgeRoutes(mux, api.KnowledgeDeps{Pool: pool, Logger: logger, Auth: dashAuth})
	api.RegisterAnalyticsRoutes(mux, api.AnalyticsDeps{Pool: pool, Logger: logger, Auth: dashAuth})
	api.RegisterAutomationsRoutes(mux, api.AutomationsDeps{Pool: pool, Logger: logger, Auth: dashAuth})
	api.RegisterEnrollAndroidRoutes(mux, api.EnrollAndroidDeps{Pool: pool, Auth: dashAuth})
	api.RegisterAndroidPolicyRoutes(mux, api.AndroidPoliciesDeps{
		Pool:    pool,
		Logger:  logger,
		Auth:    dashAuth,
		DashHub: dashHub,
		OnAndroidRemoteWipeRequested: func(ctx context.Context, assetID uuid.UUID, humanID string) {
			alerter.Notify(notifications.AlertEvent{
				Type:    notifications.EventAndroidRemoteWipe,
				Title:   "Android remote wipe requested",
				Message: fmt.Sprintf("Remote wipe was requested for asset %s (%s).", humanID, assetID.String()),
				Details: map[string]any{
					"asset_id": assetID.String(),
					"human_id": humanID,
				},
			})
		},
	})

	api.RegisterAlertRoutes(mux, api.AlertsDeps{
		Pool:    pool,
		Logger:  logger,
		Auth:    dashAuth,
		Alerter: alerter,
	})

	notifyTickets := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		tickets, err := ws.LoadTicketSnapshot(ctx, pool)
		if err != nil {
			logger.Error("ticket snapshot broadcast load failed", "err", err)
			return
		}
		b, err := json.Marshal(ws.TicketSnapshotMsg{
			Type:    ws.MsgTypeTicketSnapshot,
			Tickets: tickets,
		})
		if err != nil {
			logger.Error("ticket snapshot broadcast encode failed", "err", err)
			return
		}
		dashHub.Broadcast(b)
	}
	api.NewTicketsHandler(mux, api.TicketsDeps{
		Pool:             pool,
		Logger:           logger,
		Auth:             dashAuth,
		OnTicketsMutated: notifyTickets,
		OnINCTicketCreated: func(ctx context.Context, ticketRef, title string, linkedAssetID *uuid.UUID) {
			details := map[string]any{
				"ticket_ref": ticketRef,
				"title":      title,
			}
			if linkedAssetID != nil {
				details["asset_id"] = linkedAssetID.String()
				var hid string
				if err := pool.QueryRow(ctx, `SELECT human_id FROM assets WHERE id = $1`, *linkedAssetID).Scan(&hid); err == nil && strings.TrimSpace(hid) != "" {
					details["linked_human_id"] = hid
				}
			}
			alerter.Notify(notifications.AlertEvent{
				Type:    notifications.EventTicketINCCreated,
				Title:   "New incident ticket",
				Message: fmt.Sprintf("Incident %s created: %s", ticketRef, title),
				Details: details,
			})
		},
	})

	api.NewPackagesHandler(mux, api.PackagesDeps{
		Pool:   pool,
		Logger: logger,
		Auth:   dashAuth,
		C2Hub:  c2Hub,
	})

	api.NewFilesHandler(mux, api.FilesDeps{
		Pool:                         pool,
		Logger:                       logger,
		Auth:                         dashAuth,
		DispatchJSON:                 c2Hub.DispatchJSON,
		RegisterFSDownloadWaiter:     c2Hub.RegisterFSDownloadWaiter,
		RegisterFSUploadResultWaiter: c2Hub.RegisterFSUploadResultWaiter,
	})

	mux.HandleFunc("GET /v1/ws", ws.NewWSGatewayHandler(ws.WSGatewayDeps{
		C2Hub:            c2Hub,
		DashboardHub:     dashHub,
		Pool:             pool,
		Logger:           logger,
		MTLSRequired:     mtlsReady,
		DashboardJWT:     jwtSvc,
		DashboardOrigins: dashboardOrigins,
	}))

	serverinstall.Register(mux, logger)

	auditWrapped := auth.MutatingAuditMiddleware(pool, logger, jwtSvc, dashboardOrigins)(mux)

	srv := &http.Server{
		Addr:              listenAddr,
		Handler:           withRequestID(auditWrapped),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Minute,
		WriteTimeout:      30 * time.Minute,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
		TLSConfig:         tlsCfg,
	}

	go func() {
		if mtlsReady {
			logger.Info("https server listening", "addr", listenAddr)
			if err := srv.ListenAndServeTLS(tlsCertPath, tlsKeyPath); err != nil && !errors.Is(err, http.ErrServerClosed) {
				logger.Error("server failed", "err", err)
				os.Exit(1)
			}
		} else {
			logger.Info("http server listening", "addr", listenAddr)
			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				logger.Error("server failed", "err", err)
				os.Exit(1)
			}
		}
	}()

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)
	<-shutdown
	signal.Stop(shutdown)
	alerterCancel()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("graceful shutdown error", "err", err)
	}
	return nil
}
