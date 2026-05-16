package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ARCOOON/arx-mdm/internal/agent"
	"github.com/ARCOOON/arx-mdm/pkg/system"

	_ "github.com/ARCOOON/arx-mdm/internal/ws" // registers agent C2 command loop with cmdloop
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	if len(os.Args) >= 2 && os.Args[1] == "enterprise-wipe-worker" {
		agent.RunEnterpriseWipeWorker(logger)
		os.Exit(0)
	}

	handled, err := tryRunWindowsAgentService(logger)
	if err != nil {
		if handled {
			logger.Error("agent windows service failed", "err", err)
		} else {
			logger.Error("windows service detection failed", "err", err)
		}
		os.Exit(1)
	}
	if handled {
		return
	}

	args := os.Args[1:]
	if argsIncludeInstall(args) {
		opts, err := parseInstallOptions(argsWithoutToken(args, "-install"))
		if err != nil {
			logger.Error("invalid -install arguments", "err", err)
			printUsage()
			os.Exit(2)
		}
		if err := system.InstallWindowsAgentService(opts); err != nil {
			logger.Error("install failed", "err", err)
			os.Exit(1)
		}
		logger.Info("install completed", "service", system.AgentServiceName)
		return
	}

	if len(args) < 1 {
		printUsage()
		os.Exit(2)
	}

	switch args[0] {
	case "enroll":
		if err := runEnroll(args[1:], logger); err != nil {
			logger.Error("enroll command failed", "err", err)
			os.Exit(1)
		}
	case "run":
		if err := runAgent(args[1:], logger); err != nil {
			logger.Error("run command failed", "err", err)
			os.Exit(1)
		}
	default:
		printUsage()
		os.Exit(2)
	}
}

func argsIncludeInstall(args []string) bool {
	for _, a := range args {
		if a == "-install" {
			return true
		}
	}
	return false
}

func argsWithoutToken(args []string, tok string) []string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		if a == tok {
			continue
		}
		out = append(out, a)
	}
	return out
}

func parseInstallOptions(args []string) (system.WindowsAgentInstallOptions, error) {
	var zero system.WindowsAgentInstallOptions
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	server := fs.String("server", "", "MDM server base URL")
	certDir := fs.String("certdir", "", "directory for client.key, client.crt, root_ca.pem")
	interval := fs.Duration("interval", 0, "telemetry interval (0 = omit from service command line)")
	if err := fs.Parse(args); err != nil {
		return zero, fmt.Errorf("parse install flags: %w", err)
	}
	if *server == "" {
		return zero, errors.New("missing required -server for -install")
	}
	return system.WindowsAgentInstallOptions{
		ServerURL: *server,
		CertDir:   *certDir,
		Interval:  *interval,
	}, nil
}

func printUsage() {
	fmt.Fprintf(os.Stderr, "usage:\n")
	fmt.Fprintf(os.Stderr, "  %s enroll -server <url> -token <secret> [-certdir <path>]\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "  %s run -server <url> [-certdir <path>] [-interval <duration>]\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "    (run starts telemetry heartbeat and a persistent C2 WebSocket to /v1/ws)\n")
	fmt.Fprintf(os.Stderr, "  %s -install -server <url> [-certdir <path>] [-interval <duration>]   (Windows SCM; Administrator)\n", os.Args[0])
}

func runEnroll(args []string, logger *slog.Logger) error {
	fs := flag.NewFlagSet("enroll", flag.ExitOnError)
	fs.Usage = func() { printUsage() }
	server := fs.String("server", "", "MDM server base URL")
	token := fs.String("token", "", "enrollment presentation secret")
	certDir := fs.String("certdir", agent.DefaultCertDir(), "directory for client.key, client.crt, root_ca.pem")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parse flags: %w", err)
	}
	if *server == "" || *token == "" {
		printUsage()
		return errors.New("missing required -server or -token")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	if err := agent.Enroll(ctx, logger, agent.EnrollOptions{
		ServerURL: *server,
		Token:     *token,
		CertDir:   *certDir,
	}); err != nil {
		return err
	}

	runCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	logger.Info("enrollment complete; starting agent runtime")
	return startAgentRuntime(runCtx, logger, *server, *certDir, 60*time.Second)
}

func runAgent(args []string, logger *slog.Logger) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	fs.Usage = func() { printUsage() }
	server := fs.String("server", "", "MDM server base URL (https when server uses TLS)")
	certDir := fs.String("certdir", agent.DefaultCertDir(), "directory for client.key, client.crt, root_ca.pem")
	interval := fs.Duration("interval", 60*time.Second, "telemetry interval")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parse flags: %w", err)
	}
	if *server == "" {
		printUsage()
		return errors.New("missing required -server")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return startAgentRuntime(ctx, logger, *server, *certDir, *interval)
}

func startAgentRuntime(ctx context.Context, logger *slog.Logger, serverURL, certDir string, interval time.Duration) error {
	return agent.Run(ctx, agent.RunOptions{
		ServerURL:         serverURL,
		CertDir:           certDir,
		Logger:            logger,
		TelemetryInterval: interval,
	})
}
