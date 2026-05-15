//go:build windows

package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"log/slog"
	"time"

	"arx-mdm/internal/agent"
	"arx-mdm/internal/ws"
	"arx-mdm/pkg/system"

	"golang.org/x/sync/errgroup"
	"golang.org/x/sys/windows/svc"
)

func tryRunWindowsAgentService(logger *slog.Logger) (bool, error) {
	in, err := system.InWindowsService()
	if err != nil || !in {
		return false, err
	}
	if err := svc.Run(system.AgentServiceName, &arxAgentService{logger: logger}); err != nil {
		return true, err
	}
	return true, nil
}

type arxAgentService struct {
	logger *slog.Logger
}

func (m *arxAgentService) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	const cmdsAccepted = svc.AcceptStop | svc.AcceptShutdown

	changes <- svc.Status{State: svc.StartPending}

	runTail := extractRunArgv(args)
	if len(runTail) == 0 {
		changes <- svc.Status{State: svc.StopPending}
		return false, 1
	}

	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	server := fs.String("server", "", "MDM server base URL")
	certDir := fs.String("certdir", agent.DefaultCertDir(), "directory for client.key, client.crt, root_ca.pem")
	interval := fs.Duration("interval", 60*time.Second, "telemetry interval")
	if err := fs.Parse(runTail); err != nil {
		changes <- svc.Status{State: svc.StopPending}
		return false, 1
	}
	if *server == "" {
		changes <- svc.Status{State: svc.StopPending}
		return false, 1
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		return ws.RunClient(ctx, ws.ClientOptions{
			ServerURL: *server,
			CertDir:   *certDir,
			Logger:    m.logger,
		})
	})
	g.Go(func() error {
		return agent.RunHeartbeat(ctx, m.logger, agent.HeartbeatOptions{
			ServerURL: *server,
			CertDir:   *certDir,
			Interval:  *interval,
		})
	})

	workDone := make(chan error, 1)
	go func() { workDone <- g.Wait() }()

	changes <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}

	for {
		select {
		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				changes <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				cancel()
				err := <-workDone
				changes <- svc.Status{State: svc.StopPending}
				return exitCodeFromWait(err)
			}
		case err := <-workDone:
			cancel()
			changes <- svc.Status{State: svc.StopPending}
			return exitCodeFromWait(err)
		}
	}
}

func exitCodeFromWait(err error) (bool, uint32) {
	if err == nil || errors.Is(err, context.Canceled) {
		return false, 0
	}
	return true, 1
}

func extractRunArgv(args []string) []string {
	for i, a := range args {
		if a == "run" {
			if i+1 < len(args) {
				return args[i+1:]
			}
			return nil
		}
	}
	return nil
}
