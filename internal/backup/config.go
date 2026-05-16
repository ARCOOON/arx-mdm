package backup

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Environment keys for automated backup scheduling and storage layout.
const (
	EnvBackupStoragePath   = "ARX_BACKUP_STORAGE_PATH"
	EnvBackupCronSpec      = "ARX_BACKUP_CRON_SPEC"
	EnvBackupRetentionDays = "ARX_BACKUP_RETENTION_DAYS"
	EnvPgDumpBinary        = "ARX_PG_DUMP_PATH"
)

// Defaults when environment variables are empty.
const (
	defaultBackupRelPath    = "data/backups"
	defaultCronStandard     = "0 2 * * *"
	defaultRetentionDays    = 7
	defaultPgDumpBinary     = "pg_dump"
	maxRetentionDaysCeiling = 3650
)

// Config aggregates runtime knobs for backup creation and housekeeping.
type Config struct {
	StorageDir    string
	PKIRootAbs    string
	DatabaseURL   string
	PgDumpPath    string
	CronExpr      string
	RetentionDays int
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return strings.TrimSpace(a)
	}
	return strings.TrimSpace(b)
}

func parseRetentionDays(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultRetentionDays
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return defaultRetentionDays
	}
	if n > maxRetentionDaysCeiling {
		return maxRetentionDaysCeiling
	}
	return n
}

// LoadConfigFromEnv builds backup configuration using process environment variables.
//
// Dependencies:
// - ARX_DATABASE_URL (required externally before RunOnce)
// - ARX_PKI_STORAGE_PATH mirrored from server bootstrap via PKIRootAbs argument
func LoadConfigFromEnv(pkiRootAbs string) (Config, error) {
	storage := strings.TrimSpace(os.Getenv(EnvBackupStoragePath))
	if storage == "" {
		storage = defaultBackupRelPath
	}
	absStorage, err := filepath.Abs(storage)
	if err != nil {
		return Config{}, err
	}

	pki := strings.TrimSpace(pkiRootAbs)
	if pki == "" {
		pkiRel := strings.TrimSpace(os.Getenv("ARX_PKI_STORAGE_PATH"))
		if pkiRel == "" {
			pkiRel = "certs"
		}
		pki, err = filepath.Abs(pkiRel)
		if err != nil {
			return Config{}, err
		}
	}

	cronExpr := strings.TrimSpace(os.Getenv(EnvBackupCronSpec))
	if cronExpr == "" {
		cronExpr = defaultCronStandard
	}

	cfg := Config{
		StorageDir:    absStorage,
		PKIRootAbs:    pki,
		DatabaseURL:   strings.TrimSpace(os.Getenv("ARX_DATABASE_URL")),
		PgDumpPath:    firstNonEmpty(os.Getenv(EnvPgDumpBinary), defaultPgDumpBinary),
		CronExpr:      cronExpr,
		RetentionDays: parseRetentionDays(os.Getenv(EnvBackupRetentionDays)),
	}
	return cfg, nil
}

// RetentionCutoff returns the mod-time threshold below which backups are pruned.
func (c Config) RetentionCutoff(now time.Time) time.Time {
	if c.RetentionDays <= 0 {
		return now.Add(-time.Duration(defaultRetentionDays) * 24 * time.Hour)
	}
	return now.Add(-time.Duration(c.RetentionDays) * 24 * time.Hour)
}
