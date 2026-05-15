package database

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

var migrationNameRe = regexp.MustCompile(`^(\d+)_.*\.sql$`)

type migration struct {
	Version int
	Name    string
	SQL     string
}

func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return nil, err
	}
	var out []migration
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".sql" {
			continue
		}
		parts := migrationNameRe.FindStringSubmatch(e.Name())
		if len(parts) < 2 {
			continue
		}
		v, err := strconv.Atoi(parts[1])
		if err != nil {
			return nil, fmt.Errorf("migration filename %q: invalid version", e.Name())
		}
		b, err := migrationFiles.ReadFile(filepath.Join("migrations", e.Name()))
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", e.Name(), err)
		}
		out = append(out, migration{Version: v, Name: e.Name(), SQL: string(b)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	if len(out) == 0 {
		return nil, fmt.Errorf("no embedded migrations found")
	}
	for i := 1; i < len(out); i++ {
		if out[i].Version == out[i-1].Version {
			return nil, fmt.Errorf("duplicate migration version %d", out[i].Version)
		}
	}
	return out, nil
}

// Migrate ensures arx_schema_migrations exists and applies every embedded migration
// that has not yet been recorded. Each migration runs in its own transaction.
func Migrate(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) error {
	_, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS arx_schema_migrations (
		version bigint PRIMARY KEY NOT NULL,
		applied_at timestamptz NOT NULL DEFAULT now()
	)`)
	if err != nil {
		return fmt.Errorf("ensure arx_schema_migrations: %w", err)
	}

	migs, err := loadMigrations()
	if err != nil {
		return err
	}

	for _, mig := range migs {
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM arx_schema_migrations WHERE version = $1)`, mig.Version).Scan(&exists); err != nil {
			return fmt.Errorf("check migration %d: %w", mig.Version, err)
		}
		if exists {
			continue
		}

		tx, err := pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin migration %d: %w", mig.Version, err)
		}
		if _, err := tx.Exec(ctx, mig.SQL); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("migration %d (%s): %w", mig.Version, mig.Name, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO arx_schema_migrations (version) VALUES ($1)`, mig.Version); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("record migration %d: %w", mig.Version, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit migration %d: %w", mig.Version, err)
		}
		if log != nil {
			log.Info("database migration applied", "version", mig.Version, "file", mig.Name)
		}
	}
	return nil
}
