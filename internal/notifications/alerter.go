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
	"net/smtp"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	TypeSMTP    = "smtp"
	TypeWebhook = "webhook"

	EventAssetStaleHeartbeat = "asset_stale_heartbeat"
	EventAndroidRemoteWipe   = "android_remote_wipe"
	EventTicketINCCreated    = "ticket_inc_created"
)

// AlertEvent is a normalized alert emitted by the MDM server.
type AlertEvent struct {
	Type    string         `json:"type"`
	Title   string         `json:"title"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

// Alerter fans out AlertEvent values to configured SMTP and webhook destinations.
type Alerter struct {
	pool *pgxpool.Pool
	log  *slog.Logger

	heartbeatExpectSec int
	staleCheckInterval time.Duration

	ch chan AlertEvent
	hc  *http.Client
}

// Options configures the alerter. Zero values pick sensible defaults.
type Options struct {
	HeartbeatExpectSec int           // expected agent heartbeat interval; stale after 3× this (default 60).
	StaleCheckInterval time.Duration // how often to scan for stale assets (default 45s).
}

// NewAlerter constructs an alerter. Start must be called to run background workers.
func NewAlerter(pool *pgxpool.Pool, log *slog.Logger, opt Options) *Alerter {
	if pool == nil || log == nil {
		panic("notifications: NewAlerter requires pool and logger")
	}
	hb := opt.HeartbeatExpectSec
	if hb <= 0 {
		if v := strings.TrimSpace(os.Getenv("ARX_ALERT_HEARTBEAT_EXPECT_SEC")); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				hb = n
			}
		}
	}
	if hb <= 0 {
		hb = 60
	}
	staleInt := opt.StaleCheckInterval
	if staleInt <= 0 {
		staleInt = 45 * time.Second
	}
	return &Alerter{
		pool:                 pool,
		log:                  log,
		heartbeatExpectSec: hb,
		staleCheckInterval:   staleInt,
		ch:                   make(chan AlertEvent, 256),
		hc: &http.Client{
			Timeout: 20 * time.Second,
			Transport: &http.Transport{
				Proxy:           http.ProxyFromEnvironment,
				TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
				DialContext: (&net.Dialer{
					Timeout:   10 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
			},
		},
	}
}

// Start runs the outbound dispatch worker and stale-heartbeat scanner until ctx is done.
func (a *Alerter) Start(ctx context.Context) {
	go a.dispatchLoop(ctx)
	go a.staleLoop(ctx)
}

// Notify enqueues a single alert for asynchronous delivery.
func (a *Alerter) Notify(ev AlertEvent) {
	if strings.TrimSpace(ev.Type) == "" || strings.TrimSpace(ev.Title) == "" {
		return
	}
	select {
	case a.ch <- ev:
	default:
		a.log.Warn("notifications: alert queue full; dropping event", "type", ev.Type)
	}
}

// ClearStaleAck removes stale-heartbeat latch state after a successful telemetry heartbeat.
func (a *Alerter) ClearStaleAck(ctx context.Context, assetID uuid.UUID) {
	if a == nil || a.pool == nil {
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, _ = a.pool.Exec(cctx, `DELETE FROM alert_stale_ack WHERE asset_id = $1`, assetID)
}

func (a *Alerter) dispatchLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-a.ch:
			a.dispatchOne(context.Background(), ev)
		}
	}
}

func (a *Alerter) staleLoop(ctx context.Context) {
	t := time.NewTicker(a.staleCheckInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			a.scanStale(context.Background())
		}
	}
}

func (a *Alerter) scanStale(ctx context.Context) {
	if a.pool == nil {
		return
	}
	staleSecs := int64(a.heartbeatExpectSec * 3)
	cctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()

	rows, err := a.pool.Query(cctx, `
WITH ins AS (
  INSERT INTO alert_stale_ack (asset_id)
  SELECT a.id
  FROM assets a
  WHERE a.last_seen IS NOT NULL
    AND a.last_seen < now() - ($1::bigint * interval '1 second')
    AND NOT EXISTS (SELECT 1 FROM alert_stale_ack s WHERE s.asset_id = a.id)
  ON CONFLICT (asset_id) DO NOTHING
  RETURNING asset_id
)
SELECT ins.asset_id, a.human_id, COALESCE(NULLIF(trim(a.hostname), ''), ''), a.last_seen
FROM ins
JOIN assets a ON a.id = ins.asset_id
`, staleSecs)
	if err != nil {
		a.log.Error("notifications: stale scan failed", "err", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var assetID uuid.UUID
		var humanID, hostname string
		var lastSeen time.Time
		if err := rows.Scan(&assetID, &humanID, &hostname, &lastSeen); err != nil {
			a.log.Error("notifications: stale scan row", "err", err)
			continue
		}
		msg := fmt.Sprintf("Asset %s (%s) has not heartbeated within %d seconds (last_seen %s).",
			humanID, hostname, staleSecs, lastSeen.UTC().Format(time.RFC3339))
		a.Notify(AlertEvent{
			Type:    EventAssetStaleHeartbeat,
			Title:   "Asset missed heartbeats",
			Message: msg,
			Details: map[string]any{
				"asset_id":   assetID.String(),
				"human_id":   humanID,
				"hostname":   hostname,
				"last_seen":  lastSeen.UTC().Format(time.RFC3339),
				"threshold_s": staleSecs,
			},
		})
	}
	if err := rows.Err(); err != nil {
		a.log.Error("notifications: stale scan rows", "err", err)
	}
}

type alertSettingRow struct {
	ID         uuid.UUID
	Type       string
	ConfigJSON []byte
	IsActive   bool
}

func (a *Alerter) loadActiveSettings(ctx context.Context) ([]alertSettingRow, error) {
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	rows, err := a.pool.Query(cctx, `
SELECT id, type, config_json::text, is_active
FROM alert_settings
WHERE is_active = true
ORDER BY type ASC, created_at ASC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []alertSettingRow
	for rows.Next() {
		var r alertSettingRow
		var cfg string
		if err := rows.Scan(&r.ID, &r.Type, &cfg, &r.IsActive); err != nil {
			return nil, err
		}
		r.ConfigJSON = []byte(cfg)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (a *Alerter) dispatchOne(ctx context.Context, ev AlertEvent) {
	settings, err := a.loadActiveSettings(ctx)
	if err != nil {
		a.log.Error("notifications: load alert_settings", "err", err)
		return
	}
	if len(settings) == 0 {
		return
	}
	for _, s := range settings {
		switch s.Type {
		case TypeSMTP:
			if err := a.sendSMTP(ctx, s.ConfigJSON, ev); err != nil {
				a.log.Warn("notifications: smtp delivery failed", "setting_id", s.ID.String(), "err", err)
			}
		case TypeWebhook:
			if err := a.postWebhook(ctx, s.ConfigJSON, ev); err != nil {
				a.log.Warn("notifications: webhook delivery failed", "setting_id", s.ID.String(), "err", err)
			}
		}
	}
}

type smtpConfig struct {
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

func (a *Alerter) sendSMTP(ctx context.Context, raw []byte, ev AlertEvent) error {
	var cfg smtpConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return fmt.Errorf("smtp config: %w", err)
	}
	if strings.TrimSpace(cfg.Username) == "" {
		cfg.Username = strings.TrimSpace(cfg.User)
	}
	cfg.Host = strings.TrimSpace(cfg.Host)
	cfg.From = strings.TrimSpace(cfg.From)
	if cfg.Host == "" || cfg.From == "" || cfg.Port <= 0 {
		return errors.New("smtp: host, from, and positive port are required")
	}
	var to []string
	for _, t := range cfg.To {
		t = strings.TrimSpace(t)
		if t != "" {
			to = append(to, t)
		}
	}
	if len(to) == 0 {
		return errors.New("smtp: at least one recipient in to[] is required")
	}
	subj := ev.Title
	body := ev.Message
	if strings.TrimSpace(body) == "" {
		body = subj
	}
	msg := buildRFC822(cfg.From, to, subj, body)
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)

	if cfg.UseImplicitTLS {
		return a.sendSMTPSImplicitTLS(ctx, addr, cfg.Host, cfg.Username, cfg.Password, cfg.From, to, msg, cfg.InsecureSkipVerify)
	}

	var auth smtp.Auth
	if strings.TrimSpace(cfg.Username) != "" {
		auth = smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)
	}
	d := net.Dialer{Timeout: 15 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("smtp dial: %w", err)
	}
	defer conn.Close()
	client, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		return fmt.Errorf("smtp client: %w", err)
	}
	defer func() { _ = client.Close() }()
	if ok, _ := client.Extension("STARTTLS"); ok {
		tcfg := &tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12, InsecureSkipVerify: cfg.InsecureSkipVerify}
		if err := client.StartTLS(tcfg); err != nil {
			return fmt.Errorf("smtp starttls: %w", err)
		}
	}
	if auth != nil {
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}
	if err := client.Mail(cfg.From); err != nil {
		return fmt.Errorf("smtp mail from: %w", err)
	}
	for _, rcpt := range to {
		if err := client.Rcpt(rcpt); err != nil {
			return fmt.Errorf("smtp rcpt %s: %w", rcpt, err)
		}
	}
	wc, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	if _, err := wc.Write([]byte(msg)); err != nil {
		_ = wc.Close()
		return fmt.Errorf("smtp write: %w", err)
	}
	if err := wc.Close(); err != nil {
		return fmt.Errorf("smtp data close: %w", err)
	}
	return client.Quit()
}

func (a *Alerter) sendSMTPSImplicitTLS(ctx context.Context, addr, host, user, pass, from string, to []string, msg string, insecure bool) error {
	d := tls.Dialer{
		NetDialer: &net.Dialer{Timeout: 15 * time.Second},
		Config: &tls.Config{
			ServerName:           host,
			MinVersion:           tls.VersionTLS12,
			InsecureSkipVerify:   insecure,
		},
	}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("smtps dial: %w", err)
	}
	defer conn.Close()
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return fmt.Errorf("smtps client: %w", err)
	}
	defer func() { _ = client.Close() }()
	var auth smtp.Auth
	if strings.TrimSpace(user) != "" {
		auth = smtp.PlainAuth("", user, pass, host)
	}
	if auth != nil {
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("smtps auth: %w", err)
		}
	}
	if err := client.Mail(from); err != nil {
		return fmt.Errorf("smtps mail from: %w", err)
	}
	for _, rcpt := range to {
		if err := client.Rcpt(rcpt); err != nil {
			return fmt.Errorf("smtps rcpt %s: %w", rcpt, err)
		}
	}
	wc, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtps data: %w", err)
	}
	if _, err := wc.Write([]byte(msg)); err != nil {
		_ = wc.Close()
		return fmt.Errorf("smtps write: %w", err)
	}
	if err := wc.Close(); err != nil {
		return fmt.Errorf("smtps data close: %w", err)
	}
	return client.Quit()
}

func buildRFC822(from string, to []string, subject, body string) string {
	var b strings.Builder
	b.WriteString("From: ")
	b.WriteString(from)
	b.WriteString("\r\nTo: ")
	b.WriteString(strings.Join(to, ", "))
	b.WriteString("\r\nSubject: ")
	b.WriteString(subject)
	b.WriteString("\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n")
	b.WriteString(body)
	b.WriteString("\r\n")
	return b.String()
}

type webhookConfig struct {
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
}

func (a *Alerter) postWebhook(ctx context.Context, raw []byte, ev AlertEvent) error {
	var cfg webhookConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return fmt.Errorf("webhook config: %w", err)
	}
	cfg.URL = strings.TrimSpace(cfg.URL)
	if cfg.URL == "" {
		return errors.New("webhook: url is required")
	}
	payload, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.URL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range cfg.Headers {
		k = strings.TrimSpace(k)
		if k != "" {
			req.Header.Set(k, v)
		}
	}
	res, err := a.hc.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, res.Body)
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return fmt.Errorf("webhook: unexpected status %s", res.Status)
	}
	return nil
}

// SendTest delivers a synthetic alert using only the given alert_settings row.
func (a *Alerter) SendTest(ctx context.Context, settingID uuid.UUID) error {
	if a.pool == nil {
		return errors.New("notifications: no pool")
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var typ string
	var raw []byte
	err := a.pool.QueryRow(cctx, `
SELECT type, config_json::text FROM alert_settings WHERE id = $1
`, settingID).Scan(&typ, &raw)
	if err != nil {
		return fmt.Errorf("load setting: %w", err)
	}
	ev := AlertEvent{
		Type:    "test",
		Title:   "ARX MDM test alert",
		Message: "This is a test notification from ARX MDM alert settings.",
		Details: map[string]any{"setting_id": settingID.String()},
	}
	switch typ {
	case TypeSMTP:
		return a.sendSMTP(cctx, raw, ev)
	case TypeWebhook:
		return a.postWebhook(cctx, raw, ev)
	default:
		return fmt.Errorf("unsupported type %q", typ)
	}
}
