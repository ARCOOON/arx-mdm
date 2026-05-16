package notifications

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/ARCOOON/arx-mdm/internal/database"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Dispatcher fans out queued AlertEvents to SMTP, Slack-style, and generic webhook integrations.
type Dispatcher struct {
	pool *pgxpool.Pool
	log  *slog.Logger
	ch   chan AlertEvent

	transport *http.Transport
	hc        *http.Client
}

// NewDispatcher builds a Dispatcher instance. Invoke Start before calling Notify.
func NewDispatcher(pool *pgxpool.Pool, log *slog.Logger) *Dispatcher {
	if pool == nil || log == nil {
		panic("notifications: NewDispatcher requires pool and logger")
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if tr.TLSClientConfig == nil {
		tr.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	} else if tr.TLSClientConfig.MinVersion == 0 {
		tr.TLSClientConfig.MinVersion = tls.VersionTLS12
	}
	tr.DialContext = (&net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
	}).DialContext
	return &Dispatcher{
		pool:      pool,
		log:       log,
		ch:        make(chan AlertEvent, 512),
		transport: tr,
		hc:        &http.Client{Timeout: 20 * time.Second, Transport: tr},
	}
}

// Start consumes queued events until ctx is cancelled.
func (d *Dispatcher) Start(ctx context.Context) {
	go d.loop(ctx)
}

// Notify enqueues outbound delivery asynchronously.
func (d *Dispatcher) Notify(ev AlertEvent) {
	if strings.TrimSpace(ev.Type) == "" || strings.TrimSpace(ev.Title) == "" {
		return
	}
	select {
	case d.ch <- ev:
	default:
		d.log.Warn("notifications: queue full; dropping event", "type", ev.Type)
	}
}

func (d *Dispatcher) loop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-d.ch:
			d.deliverOnce(context.Background(), ev)
		}
	}
}

func (d *Dispatcher) deliverOnce(ctx context.Context, ev AlertEvent) {
	chRows, err := database.ListActiveNotificationChannels(ctx, d.pool)
	if err != nil {
		d.log.Error("notifications: list channels", "err", err)
		return
	}
	for _, row := range chRows {
		switch strings.TrimSpace(row.ChannelType) {
		case TypeSMTP:
			if err := sendSMTPWithGoMail(ctx, d.log, row, ev); err != nil {
				d.log.Warn("notifications: SMTP failed", "channel_id", row.ID.String(), "err", err)
			}
		case TypeWebhook:
			if err := postOutboundHook(ctx, d.hc, row, ev); err != nil {
				d.log.Warn("notifications: webhook failed", "channel_id", row.ID.String(), "err", err)
			}
		case TypeSlackIncoming:
			if err := postSlackHook(ctx, d.hc, row, ev); err != nil {
				d.log.Warn("notifications: Slack delivery failed", "channel_id", row.ID.String(), "err", err)
			}
		default:
			d.log.Warn("notifications: unsupported channel_type", "channel_type", row.ChannelType)
		}
	}
}

// SendTestChannel delivers a canned alert via one persisted notification channel row.
func (d *Dispatcher) SendTestChannel(ctx context.Context, channelID uuid.UUID) error {
	row, err := database.LoadNotificationChannelByID(ctx, d.pool, channelID)
	if err != nil || row == nil {
		return fmt.Errorf("notifications: missing channel row: %w", err)
	}
	ev := AlertEvent{
		Type:     "test",
		Severity: "info",
		Title:    "ARX MDM connectivity test",
		Message:  "Synthetic alert validating outbound routing for this channel.",
		Details:  map[string]any{"channel_id": channelID.String()},
	}
	switch strings.TrimSpace(row.ChannelType) {
	case TypeSMTP:
		return sendSMTPWithGoMail(ctx, d.log, *row, ev)
	case TypeWebhook:
		return postOutboundHook(ctx, d.hc, *row, ev)
	case TypeSlackIncoming:
		return postSlackHook(ctx, d.hc, *row, ev)
	default:
		return fmt.Errorf("notifications: unsupported channel type %q", row.ChannelType)
	}
}

type webhookChannelConfig struct {
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
}

func postOutboundHook(ctx context.Context, client *http.Client, row database.NotificationChannelRow, ev AlertEvent) error {
	var cfg webhookChannelConfig
	if err := json.Unmarshal(row.ConfigJSON, &cfg); err != nil {
		return fmt.Errorf("webhook JSON: %w", err)
	}
	cfg.URL = strings.TrimSpace(cfg.URL)
	if cfg.URL == "" {
		return errors.New("webhook: url is required")
	}
	payload, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	return dispatchSignedWebhook(ctx, client, cfg.URL, cfg.Headers, row.SigningSecret, payload)
}

type slackWebhookBody struct {
	Text   string `json:"text"`
	Mrkdwn bool   `json:"mrkdwn,omitempty"`
}

func postSlackHook(ctx context.Context, client *http.Client, row database.NotificationChannelRow, ev AlertEvent) error {
	var cfg webhookChannelConfig
	if err := json.Unmarshal(row.ConfigJSON, &cfg); err != nil {
		return fmt.Errorf("slack JSON: %w", err)
	}
	cfg.URL = strings.TrimSpace(cfg.URL)
	if cfg.URL == "" {
		return errors.New("slack: url is required")
	}
	text := "*" + escapeSlackItalic(ev.Title) + "*\n" + escapeSlackItalic(ev.Message)
	sb := slackWebhookBody{Text: text, Mrkdwn: true}
	payload, err := json.Marshal(sb)
	if err != nil {
		return err
	}
	return dispatchSignedWebhook(ctx, client, cfg.URL, cfg.Headers, row.SigningSecret, payload)
}

func escapeSlackItalic(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

func dispatchSignedWebhook(ctx context.Context, client *http.Client, urlStr string, headerMap map[string]string, secret string, body []byte) error {
	ts := strconvFormatInt(time.Now().Unix())

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, urlStr, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "arx-mdm-notifications/1.0")

	for k, v := range headerMap {
		k = strings.TrimSpace(k)
		if k != "" {
			req.Header.Set(k, v)
		}
	}

	secret = strings.TrimSpace(secret)
	if secret != "" {
		sig := hmacSha256Hex(secret, ts+"."+string(body))
		req.Header.Set("Arx-Timestamp", ts)
		req.Header.Set("Arx-Signature", fmt.Sprintf("sha256=%s", sig))
	}

	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, res.Body)
	if res.StatusCode < http.StatusOK || res.StatusCode > 299 {
		return fmt.Errorf("webhook POST status %s", res.Status)
	}
	return nil
}
