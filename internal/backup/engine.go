package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

var backupArtifactNameRX = regexp.MustCompile(`^arx-backup-[0-9]{14}UTC\.tar\.gz$`)

// Entry describes one backup archive discovered on disk.
type Entry struct {
	Filename    string    `json:"filename"`
	SizeBytes   int64     `json:"size_bytes"`
	CreatedAt   time.Time `json:"created_at"`
	PathRelRoot string    `json:"-"`
}

// Engine executes pg_dump PKI archiving, retention, and optional cron scheduling.
type Engine struct {
	cfg    Config
	log    *slog.Logger
	runMu  sync.Mutex
	cronMu sync.Mutex
	cr     *cron.Cron
}

// NewEngine verifies storage accessibility and constructs the backup engine singleton.
func NewEngine(cfg Config, log *slog.Logger) (*Engine, error) {
	if err := os.MkdirAll(cfg.StorageDir, 0o750); err != nil {
		return nil, fmt.Errorf("create backup dir: %w", err)
	}
	if cfg.PKIRootAbs == "" {
		return nil, errors.New("pki root directory is unset")
	}
	if log == nil {
		log = slog.Default()
	}
	return &Engine{cfg: cfg, log: log}, nil
}

func parseCron(spec string) (cron.Schedule, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, errors.New("empty cron expression")
	}
	if sched, err := cron.ParseStandard(spec); err == nil {
		return sched, nil
	}
	parser := cron.NewParser(cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
	return parser.Parse(spec)
}

// AttachScheduler parses ARX_BACKUP_CRON_SPEC, registers periodic backups, blocks until ctx is cancelled.
func (e *Engine) AttachScheduler(ctx context.Context) {
	if e == nil {
		return
	}
	schedule, err := parseCron(e.cfg.CronExpr)
	if err != nil {
		e.log.Error("automated backups disabled because cron expression failed to parse",
			"err", err, "spec", e.cfg.CronExpr)
		return
	}

	cr := cron.New(
		cron.WithLocation(time.UTC),
		cron.WithChain(cron.Recover(cron.DiscardLogger)),
	)

	_ = cr.Schedule(schedule, cron.FuncJob(func() {
		tctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
		defer cancel()
		name, err := e.RunOnce(tctx)
		if err != nil {
			e.log.Error("scheduled backup failed", "err", err)
			return
		}
		e.log.Info("scheduled backup finished", "filename", name)
	}))

	cr.Start()
	e.log.Info("automated backup scheduler armed", "cron", e.cfg.CronExpr)

	e.cronMu.Lock()
	e.cr = cr
	e.cronMu.Unlock()

	<-ctx.Done()

	e.stopCronUnsafe()
	e.log.Info("automated backup scheduler stopped")
}

func (e *Engine) stopCronUnsafe() {
	e.cronMu.Lock()
	cr := e.cr
	e.cr = nil
	e.cronMu.Unlock()
	if cr == nil {
		return
	}
	stopCtx := cr.Stop()
	select {
	case <-stopCtx.Done():
	case <-time.After(30 * time.Second):
	}
}

// StopCron shuts down cron without blocking on context cancellation paths.
func (e *Engine) StopCron() {
	if e == nil {
		return
	}
	e.stopCronUnsafe()
}

// RunOnce creates a gzip-compressed tarball with a pg_dump artifact and PKI mirror.
func (e *Engine) RunOnce(ctx context.Context) (finalName string, retErr error) {
	if e == nil {
		return "", errors.New("backup engine uninitialized")
	}
	if strings.TrimSpace(e.cfg.DatabaseURL) == "" {
		return "", errors.New("ARX_DATABASE_URL is required before creating backups")
	}
	e.runMu.Lock()
	defer e.runMu.Unlock()

	workDir, err := os.MkdirTemp("", "arx-backup-staging-*")
	if err != nil {
		return "", fmt.Errorf("temp staging dir: %w", err)
	}
	defer func() {
		if rmErr := os.RemoveAll(workDir); rmErr != nil && retErr == nil {
			e.log.Warn("staging cleanup incomplete", "err", rmErr, "staging_dir", workDir)
		}
	}()

	dumpFile := filepath.Join(workDir, "database.dump")
	if err := execPgDump(ctx, e.cfg.PgDumpPath, dumpFile, e.cfg.DatabaseURL); err != nil {
		return "", err
	}

	if pkiFi, statErr := os.Stat(e.cfg.PKIRootAbs); statErr != nil {
		return "", fmt.Errorf("stat pki root: %w", statErr)
	} else if !pkiFi.IsDir() {
		return "", fmt.Errorf("pki root %s is not a directory", e.cfg.PKIRootAbs)
	}

	rawName := fmt.Sprintf("arx-backup-%sUTC.tar.gz", time.Now().UTC().Format("20060102150405"))
	if !backupArtifactNameRX.MatchString(rawName) {
		return "", errors.New("internal backup naming mismatch")
	}
	stagingTar := filepath.Join(workDir, "bundle.part")
	outFile := filepath.Join(e.cfg.StorageDir, rawName)

	if err := writeBackupArchive(stagingTar, dumpFile, e.cfg.PKIRootAbs); err != nil {
		return "", err
	}

	if err := os.Rename(stagingTar, outFile); err != nil {
		return "", fmt.Errorf("publish backup bundle: %w", err)
	}

	if _, err := os.Stat(outFile); err != nil {
		return "", fmt.Errorf("published backup unreadable: %w", err)
	}

	pruneCtx, pruneCancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer pruneCancel()
	pruned, pruneErr := e.PruneOlderThan(pruneCtx, time.Now())
	if pruneErr != nil {
		e.log.Warn("backup retention pruning failed", "err", pruneErr)
	} else if pruned > 0 {
		e.log.Info("backup retention pruned stale archives", "removed", pruned, "threshold_days", e.cfg.RetentionDays)
	}

	return filepath.Base(outFile), nil
}

func execPgDump(ctx context.Context, pgDumpBin, outfile, databaseURL string) error {
	args := []string{
		"-Fc",
		"-f", outfile,
		"--no-owner",
		"--no-privileges",
		databaseURL,
	}
	cmd := exec.CommandContext(ctx, pgDumpBin, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = io.Discard

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return fmt.Errorf("pg_dump failed: %w stderr=%s", err, msg)
		}
		return fmt.Errorf("pg_dump failed: %w", err)
	}
	if _, statErr := os.Stat(outfile); statErr != nil {
		return fmt.Errorf("pg_dump output missing after success: %w", statErr)
	}
	return nil
}

func writeBackupArchive(absOutPath string, pgDumpAbs string, pkiRootAbs string) (err error) {
	f, err := os.OpenFile(absOutPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open bundle target: %w", err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	gzw := gzip.NewWriter(f)
	tw := tar.NewWriter(gzw)
	aborted := true
	defer func() {
		if aborted {
			_ = tw.Close()
			_ = gzw.Close()
		}
	}()

	dumpInfo, statErr := os.Stat(pgDumpAbs)
	if statErr != nil {
		return fmt.Errorf("stat dump file: %w", statErr)
	}
	hdr := &tar.Header{
		Typeflag: tar.TypeReg,
		Name:     filepath.ToSlash(filepath.Join("postgres", filepath.Base(pgDumpAbs))),
		Mode:     0o600,
		Size:     dumpInfo.Size(),
		ModTime:  dumpInfo.ModTime(),
		Uname:    "arx",
		Gname:    "arx",
	}
	if hdrErr := tw.WriteHeader(hdr); hdrErr != nil {
		return fmt.Errorf("dump tar header: %w", hdrErr)
	}

	dfh, dumpOpenErr := os.Open(pgDumpAbs)
	if dumpOpenErr != nil {
		return fmt.Errorf("open pg_dump output: %w", dumpOpenErr)
	}
	if _, cpErr := io.Copy(tw, dfh); cpErr != nil {
		_ = dfh.Close()
		return fmt.Errorf("write dump blob: %w", cpErr)
	}
	_ = dfh.Close()

	if walkErr := filepath.WalkDir(pkiRootAbs, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, cutErr := filepath.Rel(pkiRootAbs, path)
		if cutErr != nil {
			return cutErr
		}
		if rel == "." && d.IsDir() {
			return nil
		}

		hdrName := filepath.ToSlash(filepath.Join("embedded_pki", rel))

		entryInfo, infoErr := d.Info()
		if infoErr != nil {
			return infoErr
		}

		switch {
		case d.IsDir():
			th := &tar.Header{
				Typeflag: tar.TypeDir,
				Name:     hdrName + "/",
				Mode:     0o750,
				ModTime:  entryInfo.ModTime(),
				Uname:    "arx",
				Gname:    "arx",
			}
			return tw.WriteHeader(th)

		case d.Type().IsRegular():
			stat, statErr := os.Stat(path)
			if statErr != nil {
				return statErr
			}
			fh := &tar.Header{
				Typeflag: tar.TypeReg,
				Name:     hdrName,
				ModTime:  stat.ModTime(),
				Uname:    "arx",
				Gname:    "arx",
			}
			fh.Mode = int64(stat.Mode().Perm())
			if fh.Mode == 0 {
				fh.Mode = 0o600
			}
			fh.Size = stat.Size()
			if whErr := tw.WriteHeader(fh); whErr != nil {
				return whErr
			}
			bin, ferr := os.Open(path)
			if ferr != nil {
				return ferr
			}
			if _, ierr := io.Copy(tw, bin); ierr != nil {
				_ = bin.Close()
				return ierr
			}
			return bin.Close()

		default:
			return nil
		}
	}); walkErr != nil {
		return fmt.Errorf("walk PKI tree: %w", walkErr)
	}

	if flushErr := tw.Flush(); flushErr != nil {
		return fmt.Errorf("flush tar bundle: %w", flushErr)
	}
	if cerr := tw.Close(); cerr != nil {
		aborted = false
		_ = gzw.Close()
		return cerr
	}
	if gzErr := gzw.Close(); gzErr != nil {
		aborted = false
		return gzErr
	}
	aborted = false
	return nil
}

// List returns backup archives sorted descending by filesystem mtime then name.
func (e *Engine) List() ([]Entry, error) {
	if e == nil {
		return nil, errors.New("backup engine uninitialized")
	}
	items, readErr := os.ReadDir(e.cfg.StorageDir)
	if readErr != nil {
		return nil, fmt.Errorf("read backup dir: %w", readErr)
	}

	var rows []Entry
	for _, entry := range items {
		name := entry.Name()
		if !backupArtifactNameRX.MatchString(name) {
			continue
		}
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		abs := filepath.Join(e.cfg.StorageDir, name)
		rows = append(rows, Entry{
			Filename:    name,
			SizeBytes:   info.Size(),
			CreatedAt:   info.ModTime().UTC(),
			PathRelRoot: abs,
		})
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].CreatedAt.Equal(rows[j].CreatedAt) {
			return rows[i].Filename > rows[j].Filename
		}
		return rows[i].CreatedAt.After(rows[j].CreatedAt)
	})
	return rows, nil
}

// ResolveSafePath rejects traversal characters and verifies the tarball exists underneath storage.
func (e *Engine) ResolveSafePath(filename string) (string, error) {
	if e == nil {
		return "", errors.New("backup engine uninitialized")
	}
	name := filepath.Base(strings.TrimSpace(filename))
	if name != filename || !backupArtifactNameRX.MatchString(name) {
		return "", errors.New("invalid backup archive name")
	}
	fullPath := filepath.Clean(filepath.Join(filepath.Clean(e.cfg.StorageDir), name))
	storageRootAbs, err := filepath.Abs(e.cfg.StorageDir)
	if err != nil {
		return "", err
	}
	fullAbs, err := filepath.Abs(fullPath)
	if err != nil {
		return "", err
	}
	relPath, relErr := filepath.Rel(storageRootAbs, fullAbs)
	if relErr != nil || strings.HasPrefix(relPath, "..") {
		return "", errors.New("path escapes backup root")
	}
	st, statErr := os.Stat(fullAbs)
	if statErr != nil {
		return "", statErr
	}
	if !st.Mode().IsRegular() {
		return "", errors.New("backup path is not a regular file")
	}
	return fullAbs, nil
}

// PruneOlderThan deletes filenames matching archive pattern when modification time exceeds retention.
func (e *Engine) PruneOlderThan(ctx context.Context, now time.Time) (removed int, err error) {
	if e == nil {
		return 0, errors.New("backup engine uninitialized")
	}
	items, readErr := os.ReadDir(e.cfg.StorageDir)
	if readErr != nil {
		return 0, readErr
	}
	cutoff := e.cfg.RetentionCutoff(now)
	for _, name := range items {
		select {
		case <-ctx.Done():
			return removed, ctx.Err()
		default:
		}

		raw := name.Name()
		if !backupArtifactNameRX.MatchString(raw) {
			continue
		}
		fullAbs, rezErr := e.ResolveSafePath(raw)
		if rezErr != nil {
			continue
		}

		stat, statErr := os.Stat(fullAbs)
		if statErr != nil || !stat.Mode().IsRegular() {
			continue
		}
		if stat.ModTime().UTC().Before(cutoff.UTC()) {
			if rmErr := os.Remove(fullAbs); rmErr == nil {
				removed++
			} else if err == nil {
				err = rmErr
			}
		}
	}
	return removed, err
}
