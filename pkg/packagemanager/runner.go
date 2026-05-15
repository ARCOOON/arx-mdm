package packagemanager

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const pmRunTimeout = 45 * time.Minute

func runInstall(ctx context.Context, typ, name, version, installCmd string) error {
	argv, err := argvInstall(typ, name, version, installCmd)
	if err != nil {
		return err
	}
	return runArgv(ctx, argv)
}

func runUninstall(ctx context.Context, typ, name, version, installCmd string) error {
	argv, err := argvUninstall(typ, name, version, installCmd)
	if err != nil {
		return err
	}
	return runArgv(ctx, argv)
}

func runArgv(ctx context.Context, argv []string) error {
	if len(argv) == 0 {
		return errors.New("packagemanager: empty argv")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, pmRunTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env = minimalEnv()
	cmd.Stdin = nil
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, truncateOut(out))
	}
	return nil
}

func minimalEnv() []string {
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"SYSTEMROOT=" + os.Getenv("SYSTEMROOT"),
		"WINDIR=" + os.Getenv("WINDIR"),
		"TMP=" + os.Getenv("TMP"),
		"TEMP=" + os.Getenv("TEMP"),
		"HOME=" + os.Getenv("HOME"),
		"USERPROFILE=" + os.Getenv("USERPROFILE"),
		"LANG=" + os.Getenv("LANG"),
		"LC_ALL=" + os.Getenv("LC_ALL"),
	}
}

func truncateOut(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 4000 {
		return s[:4000] + "…"
	}
	return s
}

func argvInstall(typ, name, version, installCmd string) ([]string, error) {
	typ = strings.ToLower(strings.TrimSpace(typ))
	name = strings.TrimSpace(name)
	version = strings.TrimSpace(version)
	switch typ {
	case "winget":
		w, err := resolveWinget()
		if err != nil {
			return nil, err
		}
		if strings.HasPrefix(name, "wingetname:") {
			dn := strings.TrimSpace(strings.TrimPrefix(name, "wingetname:"))
			return []string{w, "install", "--name", dn, "--accept-package-agreements", "--accept-source-agreements", "--silent"}, nil
		}
		args := []string{w, "install", "--id", name, "--accept-package-agreements", "--accept-source-agreements", "--silent"}
		if version != "" {
			args = append(args, "--version", version)
		}
		return args, nil
	case "choco":
		c, err := resolveChoco()
		if err != nil {
			return nil, err
		}
		return []string{c, "install", name, "-y"}, nil
	case "apt":
		a, err := resolveAPT()
		if err != nil {
			return nil, err
		}
		return []string{a, "install", "-y", name}, nil
	case "dnf":
		d, err := resolveDNF()
		if err != nil {
			return nil, err
		}
		return []string{d, "install", "-y", name}, nil
	case "custom":
		return argvFromInstallCmd(installCmd)
	default:
		return nil, fmt.Errorf("packagemanager: unsupported type %q", typ)
	}
}

func argvUninstall(typ, name, version, installCmd string) ([]string, error) {
	typ = strings.ToLower(strings.TrimSpace(typ))
	name = strings.TrimSpace(name)
	switch typ {
	case "winget":
		w, err := resolveWinget()
		if err != nil {
			return nil, err
		}
		if strings.HasPrefix(name, "wingetname:") {
			dn := strings.TrimSpace(strings.TrimPrefix(name, "wingetname:"))
			return []string{w, "uninstall", "--name", dn, "--silent", "--accept-source-agreements"}, nil
		}
		return []string{w, "uninstall", "--id", name, "--silent", "--accept-source-agreements"}, nil
	case "choco":
		c, err := resolveChoco()
		if err != nil {
			return nil, err
		}
		return []string{c, "uninstall", name, "-y"}, nil
	case "apt":
		a, err := resolveAPT()
		if err != nil {
			return nil, err
		}
		return []string{a, "remove", "-y", name}, nil
	case "dnf":
		d, err := resolveDNF()
		if err != nil {
			return nil, err
		}
		return []string{d, "remove", "-y", name}, nil
	case "custom":
		return argvFromInstallCmd(installCmd)
	default:
		return nil, fmt.Errorf("packagemanager: unsupported type %q", typ)
	}
}

func argvFromInstallCmd(installCmd string) ([]string, error) {
	installCmd = strings.TrimSpace(installCmd)
	if installCmd == "" {
		return nil, errors.New("packagemanager: custom requires install_cmd")
	}
	parts := strings.Fields(installCmd)
	if len(parts) == 0 {
		return nil, errors.New("packagemanager: invalid install_cmd")
	}
	if !filepath.IsAbs(parts[0]) {
		return nil, errors.New("packagemanager: custom install_cmd must start with an absolute path")
	}
	if st, err := os.Stat(parts[0]); err != nil || st.IsDir() {
		return nil, fmt.Errorf("packagemanager: custom binary not found: %s", parts[0])
	}
	return parts, nil
}

func resolveAPT() (string, error) {
	const p = "/usr/bin/apt-get"
	if st, err := os.Stat(p); err == nil && !st.IsDir() {
		return p, nil
	}
	return "", errors.New("packagemanager: /usr/bin/apt-get not found")
}

func resolveDNF() (string, error) {
	const p = "/usr/bin/dnf"
	if st, err := os.Stat(p); err == nil && !st.IsDir() {
		return p, nil
	}
	return "", errors.New("packagemanager: /usr/bin/dnf not found")
}

func resolveChoco() (string, error) {
	const p = `C:\ProgramData\chocolatey\bin\choco.exe`
	if st, err := os.Stat(p); err == nil && !st.IsDir() {
		return p, nil
	}
	return "", errors.New("packagemanager: choco.exe not found")
}

func resolveWinget() (string, error) {
	if runtime.GOOS != "windows" {
		return "", errors.New("packagemanager: winget is only available on Windows")
	}
	pf := os.Getenv("ProgramFiles")
	if pf == "" {
		pf = `C:\Program Files`
	}
	matches, err := filepath.Glob(filepath.Join(pf, "WindowsApps", "Microsoft.DesktopAppInstaller_*", "winget.exe"))
	if err == nil && len(matches) > 0 {
		return matches[0], nil
	}
	// Fallback: local app data path used by some installs
	ld := os.Getenv("LocalAppData")
	if ld != "" {
		candidate := filepath.Join(ld, "Microsoft", "WindowsApps", "winget.exe")
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
			return candidate, nil
		}
	}
	return "", errors.New("packagemanager: winget.exe not found")
}
