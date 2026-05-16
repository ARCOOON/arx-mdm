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
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ARCOOON/arx-mdm/internal/alerting"
	"github.com/ARCOOON/arx-mdm/internal/api"
	"github.com/ARCOOON/arx-mdm/internal/auth"
	"github.com/ARCOOON/arx-mdm/internal/backup"
	"github.com/ARCOOON/arx-mdm/internal/database"
	"github.com/ARCOOON/arx-mdm/internal/itsm"
	"github.com/ARCOOON/arx-mdm/internal/notifications"
	"github.com/ARCOOON/arx-mdm/internal/pki"
	"github.com/ARCOOON/arx-mdm/internal/scheduler"
	"github.com/ARCOOON/arx-mdm/internal/serverinstall"
	"github.com/ARCOOON/arx-mdm/internal/ws"

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

	pkiStorageAbs := filepath.Clean(caAuthority.StorageDir())
	backupCfg, backupCfgErr := backup.LoadConfigFromEnv(pkiStorageAbs)
	if backupCfgErr != nil {
		return fmt.Errorf("backup configuration: %w", backupCfgErr)
	}
	backupCfg.DatabaseURL = dsn
	backupEngine, backupInitErr := backup.NewEngine(backupCfg, logger)
	if backupInitErr != nil {
		return fmt.Errorf("backup engine initialization: %w", backupInitErr)
	}
	logger.Info("disaster recovery backup engine initialized",
		"storage_dir", backupCfg.StorageDir,
		"pki_snapshot_root", filepath.Clean(backupCfg.PKIRootAbs),
		"cron_spec", backupCfg.CronExpr,
		"retention_days", backupCfg.RetentionDays,
	)
	store := auth.NewPGXEnrollmentStore(pool)
	coordinator := auth.NewEnrollmentCoordinator(store, caAuthority)

	tlsCertPath := strings.TrimSpace(os.Getenv(envTLSCert))
	tlsKeyPath := strings.TrimSpace(os.Getenv(envTLSKey))
	mtlsCAPath := strings.TrimSpace(os.Getenv(envMTLSClientCABundle))
	mtlsReady := tlsCertPath != "" && tlsKeyPath != "" && mtlsCAPath != ""

	appsRootRaw := strings.TrimSpace(os.Getenv(envAppsStoragePath))
	if appsRootRaw == "" {
		appsRootRaw = "data/apps"
	}
	appsAbs, appsErr := filepath.Abs(appsRootRaw)
	if appsErr != nil {
		return fmt.Errorf("resolve %s: %w", envAppsStoragePath, appsErr)
	}
	if mkdirErr := os.MkdirAll(appsAbs, 0o750); mkdirErr != nil {
		return fmt.Errorf("ensure apps storage directory %s: %w", appsAbs, mkdirErr)
	}
	logger.Info("app catalog artifact storage initialized", "path", appsAbs, "env", envAppsStoragePath)

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

	bgWorkersCtx, bgWorkersCancel := context.WithCancel(context.Background())
	notifDispatcher := notifications.NewDispatcher(pool, logger)
	notifDispatcher.Start(bgWorkersCtx)
	incidentHooks := &itsm.IncidentAlertBridge{Pool: pool, Log: logger}
	alertEngine := alerting.NewEngine(alerting.Dependencies{
		Pool:          pool,
		Logger:        logger,
		Dispatcher:    notifDispatcher,
		TickInterval:  30 * time.Second,
		IncidentHooks: incidentHooks,
	})
	alertEngine.Start(bgWorkersCtx)
	go scheduler.Run(bgWorkersCtx, scheduler.Deps{
		Pool:           pool,
		Hub:            c2Hub,
		Logger:         logger,
		ReloadInterval: time.Minute,
	})

	go backupEngine.AttachScheduler(bgWorkersCtx)

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

	complianceDispatch := func(certSerial string, payload any) bool {
		return c2Hub.DispatchJSON(certSerial, payload)
	}
	onTelemetryAccepted := func(certSerial, humanID string, assetID uuid.UUID, payload api.TelemetryPayload) {
		msg, err := ws.MarshalTelemetryUpdate(pool, c2Hub, certSerial, humanID, assetID, payload)
		if err != nil {
			return
		}
		dashHub.Broadcast(msg)
	}
	telemetryProcess := api.TelemetryProcessDeps{
		Pool:               pool,
		AdvisoryLockKey:    api.AdvisoryLockKeyARXClientSeq,
		Logger:             logger,
		ComplianceDispatch: complianceDispatch,
		OnHeartbeat: func(ctx context.Context, assetID uuid.UUID) {
			alertEngine.OnHeartbeat(ctx, assetID)
		},
		OnAccepted: onTelemetryAccepted,
	}

	mux := http.NewServeMux()
	api.RegisterPublicEnrollRoute(mux, api.EnrollPublicDeps{
		Logger:      logger,
		Coordinator: coordinator,
	})
	mux.HandleFunc("POST /v1/telemetry", api.NewTelemetryHandler(api.TelemetryDeps{
		Pool:               pool,
		Logger:             logger,
		MTLSRequired:       mtlsReady,
		AdvisoryLockKey:    api.AdvisoryLockKeyARXClientSeq,
		ComplianceDispatch: complianceDispatch,
		OnHeartbeat:        telemetryProcess.OnHeartbeat,
		OnTelemetryAccepted: telemetryProcess.OnAccepted,
	}))

	api.RegisterAuthRoutes(mux, api.AuthDeps{
		Pool:    pool,
		Logger:  logger,
		JWT:     jwtSvc,
		Origins: dashboardOrigins,
	})
	api.RegisterAuditRoutes(mux, api.AuditDeps{Pool: pool, Logger: logger, Auth: dashAuth})
	api.RegisterBackupRoutes(mux, api.BackupsDeps{Engine: backupEngine, Logger: logger, Auth: dashAuth})
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
			notifDispatcher.Notify(notifications.AlertEvent{
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
		Pool:       pool,
		Logger:     logger,
		Auth:       dashAuth,
		Dispatcher: notifDispatcher,
	})

	notifyIncidents := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		incidents, err := ws.LoadIncidentSnapshot(ctx, pool)
		if err != nil {
			logger.Error("incident snapshot broadcast load failed", "err", err)
			return
		}
		b, err := json.Marshal(ws.IncidentSnapshotMsg{
			Type:      ws.MsgTypeIncidentSnapshot,
			Incidents: incidents,
		})
		if err != nil {
			logger.Error("incident snapshot broadcast encode failed", "err", err)
			return
		}
		dashHub.Broadcast(b)
	}
	api.NewIncidentsHandler(mux, api.IncidentsDeps{
		Pool:               pool,
		Logger:             logger,
		Auth:               dashAuth,
		C2Hub:              c2Hub,
		OnIncidentsMutated: notifyIncidents,
		OnINCIncidentCreated: func(ctx context.Context, incidentNumber, shortDescription string, linkedAssetID *uuid.UUID) {
			details := map[string]any{
				"incident_number":   incidentNumber,
				"short_description": shortDescription,
			}
			if linkedAssetID != nil {
				details["device_id"] = linkedAssetID.String()
				var hid string
				if err := pool.QueryRow(ctx, `SELECT human_id FROM assets WHERE id = $1`, *linkedAssetID).Scan(&hid); err == nil && strings.TrimSpace(hid) != "" {
					details["linked_human_id"] = hid
				}
			}
			notifDispatcher.Notify(notifications.AlertEvent{
				Type:    notifications.EventTicketINCCreated,
				Title:   "New incident",
				Message: fmt.Sprintf("%s created: %s", incidentNumber, shortDescription),
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

	api.RegisterDeviceCommandRoutes(mux, api.DeviceCommandsDeps{
		Pool:     pool,
		Logger:   logger,
		Auth:     dashAuth,
		Dispatch: c2Hub.DispatchJSON,
		ResolveAsset: func(ctx context.Context, deviceID uuid.UUID) (string, error) {
			return api.ResolveAssetCertSerial(ctx, pool, deviceID)
		},
	})
	api.RegisterDeviceSecurityRoutes(mux, api.DeviceSecurityDeps{
		Pool:     pool,
		Logger:   logger,
		Auth:     dashAuth,
		Dispatch: c2Hub.DispatchJSON,
		ResolveAsset: func(ctx context.Context, deviceID uuid.UUID) (string, error) {
			return api.ResolveAssetCertSerial(ctx, pool, deviceID)
		},
	})
	api.RegisterTenantComplianceRoutes(mux, api.TenantComplianceDeps{
		Pool:   pool,
		Logger: logger,
		Auth:   dashAuth,
	})
	api.RegisterDeviceQuarantineRoutes(mux, api.DeviceQuarantineDeps{
		Pool:     pool,
		Logger:   logger,
		Auth:     dashAuth,
		Dispatch: c2Hub.DispatchJSON,
		ResolveAsset: func(ctx context.Context, deviceID uuid.UUID) (string, error) {
			return api.ResolveAssetCertSerial(ctx, pool, deviceID)
		},
	})

	api.RegisterDeviceAssignmentRoutes(mux, api.DeviceAssignmentsDeps{
		Pool:   pool,
		Logger: logger,
		Auth:   dashAuth,
	})

	api.RegisterAgentAppArtifactRoutes(mux, api.AgentAppArtifactDeps{
		Pool:         pool,
		Logger:       logger,
		MTLSRequired: mtlsReady,
		AppsRoot:     appsAbs,
	})
	api.RegisterAgentProfilesRoutes(mux, api.AgentProfilesDeps{
		Pool:         pool,
		Logger:       logger,
		MTLSRequired: mtlsReady,
	})
	epDeps := api.EffectivePolicyDeps{
		Pool:         pool,
		Logger:       logger,
		Auth:         dashAuth,
		MTLSRequired: mtlsReady,
	}
	api.RegisterEffectivePolicyRoutes(mux, epDeps)
	api.RegisterAgentEffectivePolicyRoutes(mux, epDeps)
	api.RegisterAppCatalogRoutes(mux, api.AppCatalogDeps{
		Pool:     pool,
		Logger:   logger,
		Auth:     dashAuth,
		C2Hub:    c2Hub,
		AppsRoot: appsAbs,
	})
	api.RegisterManagedAppConfigurationRoutes(mux, api.AppManagedConfigDeps{
		Pool:   pool,
		Logger: logger,
		Auth:   dashAuth,
	})
	api.RegisterConfigurationProfilesRoutes(mux, api.ConfigurationProfilesDeps{
		Pool:   pool,
		Logger: logger,
		Auth:   dashAuth,
	})
	api.RegisterPrincipalGroupRoutes(mux, api.PrincipalGroupsDeps{
		Pool:   pool,
		Logger: logger,
		Auth:   dashAuth,
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
		AgentTelemetry: ws.AgentTelemetryDeps{
			Pool:               pool,
			Logger:             logger,
			AdvisoryLockKey:    api.AdvisoryLockKeyARXClientSeq,
			ComplianceDispatch: telemetryProcess.ComplianceDispatch,
			OnHeartbeat:        telemetryProcess.OnHeartbeat,
			OnAccepted:         telemetryProcess.OnAccepted,
		},
	}))

	serverinstall.Register(mux)
	api.RegisterEmbeddedStaticUI(mux, logger)

	h := auth.MutatingAuditMiddleware(pool, logger, jwtSvc, dashboardOrigins)(mux)
	h = auth.DashboardRBACMiddleware(jwtSvc, dashboardOrigins)(h)
	auditWrapped := h

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
	bgWorkersCancel()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("graceful shutdown error", "err", err)
	}
	return nil
}
