package notifications

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ARCOOON/arx-mdm/internal/database"
	wmail "github.com/wneessen/go-mail"
)

type smtpDeliveryConfig struct {
	Host               string   `json:"host"`
	Port               int      `json:"port"`
	Username           string   `json:"username"`
	User               string   `json:"user"`
	Password           string   `json:"password"`
	From               string   `json:"from"`
	To                 []string `json:"to"`
	UseImplicitTLS     bool     `json:"use_implicit_tls"`
	InsecureSkipVerify bool     `json:"insecure_skip_verify"`
}

func sendSMTPWithGoMail(ctx context.Context, log *slog.Logger, row database.NotificationChannelRow, ev AlertEvent) error {
	if log == nil {
		log = slog.Default()
	}
	var cfg smtpDeliveryConfig
	if err := json.Unmarshal(row.ConfigJSON, &cfg); err != nil {
		return fmt.Errorf("smtp JSON: %w", err)
	}
	if strings.TrimSpace(cfg.Username) == "" {
		cfg.Username = strings.TrimSpace(cfg.User)
	}
	cfg.Host = strings.TrimSpace(cfg.Host)
	cfg.From = strings.TrimSpace(cfg.From)
	if cfg.Host == "" || cfg.From == "" || cfg.Port <= 0 {
		return errors.New("smtp: host, from, and positive port are required")
	}
	var recipients []string
	for _, addr := range cfg.To {
		addr = strings.TrimSpace(addr)
		if addr != "" {
			recipients = append(recipients, addr)
		}
	}
	if len(recipients) == 0 {
		return errors.New("smtp: at least one recipient is required")
	}
	subject := strings.TrimSpace(ev.Title)
	body := strings.TrimSpace(ev.Message)
	if body == "" {
		body = subject
	}
	msg := wmail.NewMsg()
	if err := msg.From(cfg.From); err != nil {
		return fmt.Errorf("smtp from: %w", err)
	}
	for _, rcpt := range recipients {
		if err := msg.To(rcpt); err != nil {
			return fmt.Errorf("smtp to: %w", err)
		}
	}
	msg.Subject(subject)
	msg.SetBodyString(wmail.TypeTextPlain, body)

	opts := []wmail.Option{
		wmail.WithTimeout(45 * time.Second),
		wmail.WithPort(cfg.Port),
		wmail.WithTLSConfig(&tls.Config{
			ServerName:         cfg.Host,
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: cfg.InsecureSkipVerify,
		}),
	}
	if cfg.UseImplicitTLS {
		opts = append(opts, wmail.WithSSL())
		if cfg.Username != "" {
			opts = append(opts,
				wmail.WithSMTPAuth(wmail.SMTPAuthPlain),
				wmail.WithUsername(cfg.Username),
				wmail.WithPassword(cfg.Password),
			)
		} else {
			opts = append(opts, wmail.WithSMTPAuth(wmail.SMTPAuthNoAuth))
		}
	} else {
		opts = append(opts, wmail.WithTLSPolicy(wmail.TLSMandatory))
		if cfg.Username != "" {
			opts = append(opts,
				wmail.WithSMTPAuth(wmail.SMTPAuthPlain),
				wmail.WithUsername(cfg.Username),
				wmail.WithPassword(cfg.Password),
			)
		} else {
			opts = append(opts, wmail.WithSMTPAuth(wmail.SMTPAuthNoAuth))
		}
	}

	cli, err := wmail.NewClient(cfg.Host, opts...)
	if err != nil {
		return fmt.Errorf("smtp client: %w", err)
	}

	if err := cli.DialAndSendWithContext(ctx, msg); err != nil {
		return fmt.Errorf("smtp send: %w", err)
	}
	return nil
}
