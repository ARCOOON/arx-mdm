package system

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DirEntryInfo describes one child of a directory for operator tooling.
type DirEntryInfo struct {
	Name          string `json:"name"`
	IsDir         bool   `json:"is_dir"`
	Size          int64  `json:"size"`
	ModTimeUnix   int64  `json:"mod_time_unix"`
	ModePermOctal uint32 `json:"mode_perm_octal"`
}

// SanitizePath rejects NUL bytes and returns a cleaned path for native OS APIs.
func SanitizePath(p string) (string, error) {
	if strings.TrimSpace(p) == "" {
		return "", fmt.Errorf("path is required")
	}
	if strings.ContainsRune(p, '\x00') {
		return "", fmt.Errorf("path contains NUL")
	}
	return filepath.Clean(p), nil
}

// ListDir returns non-recursive directory entries using os.ReadDir.
func ListDir(path string) ([]DirEntryInfo, error) {
	path, err := SanitizePath(path)
	if err != nil {
		return nil, err
	}
	ents, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	out := make([]DirEntryInfo, 0, len(ents))
	for _, e := range ents {
		info, err := e.Info()
		if err != nil {
			out = append(out, DirEntryInfo{Name: e.Name(), IsDir: e.IsDir()})
			continue
		}
		mode := info.Mode()
		out = append(out, DirEntryInfo{
			Name:          e.Name(),
			IsDir:         e.IsDir(),
			Size:         info.Size(),
			ModTimeUnix:  info.ModTime().Unix(),
			ModePermOctal: uint32(mode.Perm()),
		})
	}
	return out, nil
}

// ReadFileBytes reads an entire file (caller should enforce size limits for untrusted paths).
func ReadFileBytes(path string) ([]byte, error) {
	path, err := SanitizePath(path)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

// WriteFileBytes writes data to path with the given permission bits (before umask).
func WriteFileBytes(path string, data []byte, perm fs.FileMode) error {
	path, err := SanitizePath(path)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, perm)
}

// OpenFileWriteTrunc opens a file for write, truncating if it exists.
func OpenFileWriteTrunc(path string, perm fs.FileMode) (*os.File, error) {
	path, err := SanitizePath(path)
	if err != nil {
		return nil, err
	}
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
}

// FileModTime returns modification time or zero time on stat error.
func FileModTime(path string) time.Time {
	path, err := SanitizePath(path)
	if err != nil {
		return time.Time{}
	}
	st, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return st.ModTime()
}
