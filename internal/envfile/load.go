// Package envfile loads KEY=VALUE pairs from a .env file into the process environment.
package envfile

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LoadFromCWD reads .env from the process current working directory.
// Variables already set in the environment are not overwritten.
func LoadFromCWD() error {
	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("envfile: getwd: %w", err)
	}
	return Load(filepath.Join(wd, ".env"))
}

// Load reads the file at path when it exists; a missing file is not an error.
func Load(path string) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("envfile: open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		val = strings.TrimSpace(val)
		val = strings.Trim(val, `"'`)
		if os.Getenv(key) == "" {
			_ = os.Setenv(key, val)
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("envfile: read %s: %w", path, err)
	}
	return nil
}
