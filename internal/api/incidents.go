package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ARCOOON/arx-mdm/internal/auth"
	"github.com/ARCOOON/arx-mdm/internal/database"
	"github.com/ARCOOON/arx-mdm/internal/models"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const maxIncidentJSONBody = 512 << 10

// C2IncidentHub is the subset of ws.Hub used by incident workflows (avoid import cycles with internal/ws).
type C2IncidentHub interface {
	ConnectedCertSerials() []string
	DispatchJSON(certSerial string, payload any) bool
}

// IncidentsDeps wires the Service Desk incidents REST surface.
type IncidentsDeps struct {
	Pool                 *pgxpool.Pool
	Logger               *slog.Logger
	Auth                 DashboardAuth
	OnIncidentsMutated   func()
	C2Hub                C2IncidentHub
	OnINCIncidentCreated func(ctx context.Context, incidentNumber, shortDescription string, linkedAssetID *uuid.UUID)
}

// IncidentListRow is returned by GET /v1/incidents.
type IncidentListRow struct {
	ID                     uuid.UUID  `json:"id"`
	IncidentNumber         string     `json:"incident_number"`
	ShortDescription       string     `json:"short_description"`
	State                  string     `json:"state"`
	Priority               int        `json:"priority"`
	Impact                 int        `json:"impact"`
	Urgency                int        `json:"urgency"`
	SLADue                 time.Time  `json:"sla_due"`
	SourceAlertFingerprint *string    `json:"source_alert_fingerprint,omitempty"`
	LinkedArxID            *string    `json:"linked_arx_id,omitempty"`
	CMDBDeviceID           *uuid.UUID `json:"cmdb_ci,omitempty"`
	CreatedAt              time.Time  `json:"created_at"`
	UpdatedAt              time.Time  `json:"updated_at"`
	CallerUserID           *uuid.UUID `json:"caller_id,omitempty"`
	CallerUsername         *string    `json:"caller_username,omitempty"`
	AssignedToUserID       *uuid.UUID `json:"assigned_to,omitempty"`
	AssignedToUsername     *string    `json:"assigned_to_username,omitempty"`
}

// IncidentCMDBWire is live-ish CMDB telemetry returned with incident GET.
type IncidentCMDBWire struct {
	DeviceID            uuid.UUID `json:"device_id"`
	HumanID             string    `json:"human_id"`
	Hostname            *string   `json:"hostname,omitempty"`
	OperationalStatus   string    `json:"operational_status"`
	CostCenter          string    `json:"cost_center"`
	Location            string    `json:"location"`
	LastSeenRFC3339     *string   `json:"last_seen,omitempty"`
	C2Connected         bool      `json:"c2_connected"`
	CPUPercent          *float64  `json:"cpu_usage_percent,omitempty"`
	RAMPercent          *float64  `json:"ram_usage_percent,omitempty"`
	DiskPercent         *float64  `json:"disk_usage_percent,omitempty"`
	LatestMetricsRFC333 *string   `json:"latest_metrics_at,omitempty"`
	RecentC2Patterns    []string  `json:"recent_c2_command_types,omitempty"`
	C2Capabilities      []string  `json:"c2_capabilities"`
}

// IncidentDetailResponse is returned by GET /v1/incidents/{id}.
type IncidentDetailResponse struct {
	Incident    IncidentListRow     `json:"incident"`
	WorkNotes   json.RawMessage     `json:"work_notes"`
	CMDBContext *IncidentCMDBWire   `json:"cmdb_context,omitempty"`
	Resolutions []models.Resolution `json:"resolutions"`
}

type createIncidentRequest struct {
	ShortDescription   string `json:"short_description"`
	State              string `json:"state"`
	Impact             *int   `json:"impact"`
	Urgency            *int   `json:"urgency"`
	LinkedAssetHumanID string `json:"linked_asset_human_id"`
	AssignedToUserID   string `json:"assigned_to_user_id"`
	InitialNote        string `json:"initial_work_note"`
	SLADueRFC3339      string `json:"sla_due"`
}

type patchIncidentRequest struct {
	ShortDescription   *string `json:"short_description"`
	State              *string `json:"state"`
	Impact             *int    `json:"impact"`
	Urgency            *int    `json:"urgency"`
	LinkedAssetHumanID *string `json:"linked_asset_human_id"`
	AssignedToUserID   *string `json:"assigned_to_user_id"`
	ClearAssignedTo    *bool   `json:"clear_assigned_to"`
	AppendWorkNote     *string `json:"append_work_note"`
	SLADueRFC3339      *string `json:"sla_due"`
}

type createResolutionRequest struct {
	Summary  string `json:"summary"`
	Markdown string `json:"markdown"`
}

type createIncidentCommandBody struct {
	CommandType string `json:"command_type"`
	Payload     string `json:"payload"`
}

var allowedIncidentState = map[string]struct{}{
	"new": {}, "in_progress": {}, "on_hold": {}, "resolved": {}, "closed": {},
}

func normalizeIncidentState(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func validateIncidentState(s string) bool {
	_, ok := allowedIncidentState[s]
	return ok
}

func impactUrgencyOrDefault(v *int) int16 {
	if v == nil {
		return 2
	}
	x := int16(*v)
	if x < 1 {
		x = 1
	}
	if x > 3 {
		x = 3
	}
	return x
}

func hubCertConnected(h C2IncidentHub, certSerial string) bool {
	if h == nil {
		return false
	}
	certSerial = strings.TrimSpace(certSerial)
	if certSerial == "" {
		return false
	}
	for _, s := range h.ConnectedCertSerials() {
		if strings.TrimSpace(s) == certSerial {
			return true
		}
	}
	return false
}

func buildCMDBWire(h C2IncidentHub, snap *database.IncidentCMDBSnapshot) *IncidentCMDBWire {
	if snap == nil {
		return nil
	}
	w := IncidentCMDBWire{
		DeviceID:          snap.DeviceID,
		HumanID:           snap.HumanID,
		Hostname:          snap.Hostname,
		OperationalStatus: snap.OperationalStatus,
		CostCenter:        snap.CostCenter,
		Location:          snap.Location,
		RecentC2Patterns:  snap.RecentCommandTypes,
		C2Capabilities: []string{
			models.DeviceCommandTypePing,
			models.DeviceCommandTypeReboot,
			models.DeviceCommandTypeScript,
			models.DeviceCommandTypeRestartService,
			models.DeviceCommandTypePushConfig,
		},
	}
	if snap.LastSeen != nil {
		s := snap.LastSeen.UTC().Format(time.RFC3339)
		w.LastSeenRFC3339 = &s
	}
	if snap.LatestMetricsAt != nil {
		s := snap.LatestMetricsAt.UTC().Format(time.RFC3339)
		w.LatestMetricsRFC333 = &s
	}
	w.CPUPercent = snap.CpuUsagePercent
	w.RAMPercent = snap.RAMPercent
	w.DiskPercent = snap.DiskPercent
	if snap.CertSerial != nil && strings.TrimSpace(*snap.CertSerial) != "" {
		w.C2Connected = hubCertConnected(h, strings.TrimSpace(*snap.CertSerial))
	}
	return &w
}

// NewIncidentsHandler registers dashboard incident CRUD and C2 command bridge.
func NewIncidentsHandler(mux *http.ServeMux, d IncidentsDeps) {
	if d.Pool == nil || d.Logger == nil || d.Auth.JWT == nil {
		panic("api: incidents handler requires Pool, Logger, and Auth.JWT")
	}
	h := &incidentsHandler{deps: d}
	mux.HandleFunc("GET /v1/incidents", h.handleList)
	mux.HandleFunc("POST /v1/incidents", h.handleCreate)
	mux.HandleFunc("GET /v1/incidents/{id}", h.handleGet)
	mux.HandleFunc("PATCH /v1/incidents/{id}", h.handlePatch)
	mux.HandleFunc("DELETE /v1/incidents/{id}", h.handleDelete)
	mux.HandleFunc("POST /v1/incidents/{id}/resolutions", h.handlePostResolution)
	mux.HandleFunc("POST /v1/incidents/{id}/commands", h.handlePostIncidentCommand)
}

type incidentsHandler struct {
	deps IncidentsDeps
}

func (h *incidentsHandler) authorizeViewer(w http.ResponseWriter, r *http.Request) bool {
	_, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleViewer)
	return ok
}

func (h *incidentsHandler) authorizeOperator(w http.ResponseWriter, r *http.Request) bool {
	_, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleOperator)
	return ok
}

func (h *incidentsHandler) notifyMutated() {
	if h.deps.OnIncidentsMutated == nil {
		return
	}
	defer func() {
		if rec := recover(); rec != nil {
			h.deps.Logger.Error("OnIncidentsMutated panicked", "recover", rec)
		}
	}()
	h.deps.OnIncidentsMutated()
}

func (h *incidentsHandler) handleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !h.authorizeViewer(w, r) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	var filterArg any
	if q := strings.TrimSpace(r.URL.Query().Get("device_id")); q != "" {
		id, err := uuid.Parse(q)
		if err != nil {
			writeTicketsError(w, http.StatusBadRequest, "invalid device_id query parameter")
			return
		}
		filterArg = id
	}

	rows, err := h.deps.Pool.Query(ctx, `
SELECT i.id, i.incident_number, i.short_description, i.state, i.priority, i.impact, i.urgency, i.sla_due,
       i.source_alert_fingerprint,
       i.cmdb_ci, i.created_at, i.updated_at, a.human_id,
       i.caller_id, cb.username,
       i.assigned_to, ab.username
FROM incidents i
LEFT JOIN assets a ON a.id = i.cmdb_ci
LEFT JOIN users cb ON cb.id = i.caller_id
LEFT JOIN users ab ON ab.id = i.assigned_to
WHERE ($1::uuid IS NULL OR i.cmdb_ci = $1)
ORDER BY i.created_at DESC
LIMIT 1000
`, filterArg)
	if err != nil {
		h.deps.Logger.Error("list incidents", "err", err, "request_id", r.Header.Get("X-Request-Id"))
		writeTicketsError(w, http.StatusInternalServerError, "failed to list incidents")
		return
	}
	defer rows.Close()

	out := []IncidentListRow{}
	for rows.Next() {
		var row IncidentListRow
		var humanID *string
		var caller *uuid.UUID
		var callerName *string
		var assigned *uuid.UUID
		var assignedName *string
		var fp *string
		if err := rows.Scan(
			&row.ID, &row.IncidentNumber, &row.ShortDescription, &row.State, &row.Priority, &row.Impact, &row.Urgency, &row.SLADue,
			&fp,
			&row.CMDBDeviceID,
			&row.CreatedAt, &row.UpdatedAt, &humanID,
			&caller, &callerName,
			&assigned, &assignedName,
		); err != nil {
			h.deps.Logger.Error("scan incident", "err", err)
			writeTicketsError(w, http.StatusInternalServerError, "failed to list incidents")
			return
		}
		if fp != nil && strings.TrimSpace(*fp) != "" {
			s := strings.TrimSpace(*fp)
			row.SourceAlertFingerprint = &s
		}
		if humanID != nil && strings.TrimSpace(*humanID) != "" {
			h := strings.TrimSpace(*humanID)
			row.LinkedArxID = &h
		}
		if caller != nil {
			row.CallerUserID = caller
			if callerName != nil && *callerName != "" {
				row.CallerUsername = callerName
			}
		}
		if assigned != nil {
			row.AssignedToUserID = assigned
			if assignedName != nil && *assignedName != "" {
				row.AssignedToUsername = assignedName
			}
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		writeTicketsError(w, http.StatusInternalServerError, "failed to list incidents")
		return
	}
	writeTicketsJSON(w, http.StatusOK, out)
}

func (h *incidentsHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	p, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleOperator)
	if !ok {
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxIncidentJSONBody))
	_ = r.Body.Close()
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "could not read request body")
		return
	}
	var req createIncidentRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.ShortDescription = strings.TrimSpace(req.ShortDescription)
	if req.ShortDescription == "" {
		writeTicketsError(w, http.StatusBadRequest, "short_description is required")
		return
	}
	st := normalizeIncidentState(req.State)
	if st == "" {
		st = "new"
	}
	if !validateIncidentState(st) {
		writeTicketsError(w, http.StatusBadRequest, "invalid state")
		return
	}
	impact := impactUrgencyOrDefault(req.Impact)
	urgency := impactUrgencyOrDefault(req.Urgency)

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	tx, err := h.deps.Pool.Begin(ctx)
	if err != nil {
		writeTicketsError(w, http.StatusInternalServerError, "failed to create incident")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var incNum string
	if qerr := tx.QueryRow(ctx, `
SELECT 'INC' || LPAD(nextval('incident_seq'::regclass)::TEXT, 7, '0')
`).Scan(&incNum); qerr != nil {
		h.deps.Logger.Error("incident seq", "err", qerr)
		writeTicketsError(w, http.StatusInternalServerError, "failed to allocate incident number")
		return
	}
	var cmdb *uuid.UUID
	if hid := strings.TrimSpace(req.LinkedAssetHumanID); hid != "" {
		var aid uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT id FROM assets WHERE human_id = $1`, hid).Scan(&aid); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeTicketsError(w, http.StatusBadRequest, "linked_asset_human_id not found")
				return
			}
			writeTicketsError(w, http.StatusInternalServerError, "failed to resolve asset")
			return
		}
		cmdb = &aid
	}

	var assignedTo *uuid.UUID
	if au := strings.TrimSpace(req.AssignedToUserID); au != "" {
		aid, err := uuid.Parse(au)
		if err != nil {
			writeTicketsError(w, http.StatusBadRequest, "invalid assigned_to_user_id")
			return
		}
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id = $1)`, aid).Scan(&exists); err != nil || !exists {
			writeTicketsError(w, http.StatusBadRequest, "assigned_to_user_id not found")
			return
		}
		assignedTo = &aid
	}

	sla := time.Now().UTC().Add(8 * time.Hour)
	if raw := strings.TrimSpace(req.SLADueRFC3339); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeTicketsError(w, http.StatusBadRequest, "sla_due must be RFC3339")
			return
		}
		sla = t.UTC()
	}

	notesJSON := `[]`
	if nt := strings.TrimSpace(req.InitialNote); nt != "" {
		note := map[string]any{
			"ts":          time.Now().UTC().Format(time.RFC3339),
			"author_type": "operator",
			"kind":        "manual",
			"text":        nt,
			"user_id":     p.UserID.String(),
		}
		blob, err := json.Marshal([]any{note})
		if err != nil {
			writeTicketsError(w, http.StatusBadRequest, "could not marshal work_notes")
			return
		}
		notesJSON = string(blob)
	}

	var newID uuid.UUID
	err = tx.QueryRow(ctx, `
INSERT INTO incidents (
  incident_number, caller_id, assigned_to, cmdb_ci,
  state, impact, urgency, short_description,
  work_notes, sla_due
) VALUES ($1, $2, $3, $4,
  $5, $6, $7, left($8, 500),
  $9::jsonb, $10
)
RETURNING id
`, incNum, p.UserID, assignedTo, cmdb,
		st, impact, urgency, req.ShortDescription,
		notesJSON, sla).Scan(&newID)
	if err != nil {
		h.deps.Logger.Error("insert incident", "err", err)
		writeTicketsError(w, http.StatusInternalServerError, "failed to create incident")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeTicketsError(w, http.StatusInternalServerError, "failed to create incident")
		return
	}

	h.notifyMutated()
	if h.deps.OnINCIncidentCreated != nil {
		evCtx, evCancel := context.WithTimeout(context.Background(), 5*time.Second)
		go func() {
			defer evCancel()
			defer func() {
				if rec := recover(); rec != nil {
					h.deps.Logger.Error("OnINCIncidentCreated panicked", "recover", rec)
				}
			}()
			h.deps.OnINCIncidentCreated(evCtx, incNum, req.ShortDescription, cmdb)
		}()
	}
	writeTicketsJSON(w, http.StatusCreated, map[string]any{"id": newID, "incident_number": incNum})
}

func parseIncidentUUID(r *http.Request) (uuid.UUID, error) {
	raw := strings.TrimSpace(r.PathValue("id"))
	if raw == "" {
		return uuid.Nil, fmt.Errorf("missing id")
	}
	return uuid.Parse(raw)
}

func (h *incidentsHandler) loadIncidentDetail(ctx context.Context, id uuid.UUID) (IncidentDetailResponse, error) {
	var out IncidentDetailResponse
	var workRaw []byte
	row := h.deps.Pool.QueryRow(ctx, `
SELECT i.id, i.incident_number, i.short_description, i.state, i.priority, i.impact, i.urgency, i.sla_due,
       i.source_alert_fingerprint,
       i.cmdb_ci, i.created_at, i.updated_at, a.human_id,
       i.caller_id, cb.username,
       i.assigned_to, ab.username,
       coalesce(i.work_notes::text, '[]')
FROM incidents i
LEFT JOIN assets a ON a.id = i.cmdb_ci
LEFT JOIN users cb ON cb.id = i.caller_id
LEFT JOIN users ab ON ab.id = i.assigned_to
WHERE i.id = $1
`, id)
	var humanID *string
	var caller *uuid.UUID
	var callerName *string
	var assigned *uuid.UUID
	var assignedName *string
	var fp *string
	err := row.Scan(
		&out.Incident.ID, &out.Incident.IncidentNumber, &out.Incident.ShortDescription, &out.Incident.State,
		&out.Incident.Priority, &out.Incident.Impact, &out.Incident.Urgency, &out.Incident.SLADue,
		&fp,
		&out.Incident.CMDBDeviceID, &out.Incident.CreatedAt, &out.Incident.UpdatedAt, &humanID,
		&caller, &callerName,
		&assigned, &assignedName,
		&workRaw,
	)
	if err != nil {
		return out, err
	}
	if fp != nil && strings.TrimSpace(*fp) != "" {
		s := strings.TrimSpace(*fp)
		out.Incident.SourceAlertFingerprint = &s
	}
	if humanID != nil && strings.TrimSpace(*humanID) != "" {
		hid := strings.TrimSpace(*humanID)
		out.Incident.LinkedArxID = &hid
	}
	if caller != nil {
		out.Incident.CallerUserID = caller
		if callerName != nil && *callerName != "" {
			out.Incident.CallerUsername = callerName
		}
	}
	if assigned != nil {
		out.Incident.AssignedToUserID = assigned
		if assignedName != nil && *assignedName != "" {
			out.Incident.AssignedToUsername = assignedName
		}
	}
	if len(workRaw) > 0 {
		out.WorkNotes = append(json.RawMessage(nil), workRaw...)
	} else {
		out.WorkNotes = json.RawMessage(`[]`)
	}

	if out.Incident.CMDBDeviceID != nil && *out.Incident.CMDBDeviceID != uuid.Nil {
		snap, err := database.LoadIncidentCMDBSnapshot(ctx, h.deps.Pool, *out.Incident.CMDBDeviceID)
		if err == nil && snap != nil {
			out.CMDBContext = buildCMDBWire(h.deps.C2Hub, snap)
		}
	}

	rrows, err := h.deps.Pool.Query(ctx, `
SELECT id, incident_id, summary, COALESCE(markdown, ''), details, resolved_at, created_at
FROM resolutions
WHERE incident_id = $1
ORDER BY created_at ASC, id ASC
`, id)
	if err != nil {
		return out, err
	}
	defer rrows.Close()
	for rrows.Next() {
		var res models.Resolution
		if err := rrows.Scan(&res.ID, &res.IncidentID, &res.Summary, &res.Markdown, &res.Details, &res.ResolvedAt, &res.CreatedAt); err != nil {
			return out, err
		}
		res.Details = nil
		out.Resolutions = append(out.Resolutions, res)
	}
	return out, rrows.Err()
}

func (h *incidentsHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !h.authorizeViewer(w, r) {
		return
	}
	id, err := parseIncidentUUID(r)
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid incident id")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	detail, err := h.loadIncidentDetail(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeTicketsError(w, http.StatusNotFound, "incident not found")
		return
	}
	if err != nil {
		h.deps.Logger.Error("get incident", "err", err)
		writeTicketsError(w, http.StatusInternalServerError, "failed to load incident")
		return
	}
	writeTicketsJSON(w, http.StatusOK, detail)
}

func (h *incidentsHandler) handlePatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	p, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleOperator)
	if !ok {
		return
	}
	id, err := parseIncidentUUID(r)
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid incident id")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxIncidentJSONBody))
	_ = r.Body.Close()
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "could not read request body")
		return
	}
	var req patchIncidentRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.ShortDescription == nil && req.State == nil && req.Impact == nil && req.Urgency == nil &&
		req.LinkedAssetHumanID == nil && req.AssignedToUserID == nil && req.ClearAssignedTo == nil &&
		req.AppendWorkNote == nil && req.SLADueRFC3339 == nil {
		writeTicketsError(w, http.StatusBadRequest, "no fields to update")
		return
	}
	if req.ShortDescription != nil {
		*req.ShortDescription = strings.TrimSpace(*req.ShortDescription)
		if *req.ShortDescription == "" {
			writeTicketsError(w, http.StatusBadRequest, "short_description cannot be empty")
			return
		}
	}
	if req.State != nil {
		ss := normalizeIncidentState(*req.State)
		if !validateIncidentState(ss) {
			writeTicketsError(w, http.StatusBadRequest, "invalid state")
			return
		}
		req.State = &ss
	}
	if req.ClearAssignedTo != nil && *req.ClearAssignedTo && strings.TrimSpace(pointerStr(req.AssignedToUserID)) != "" {
		writeTicketsError(w, http.StatusBadRequest, "clear_assigned_to conflicts with assigned_to_user_id")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()

	tx, err := h.deps.Pool.Begin(ctx)
	if err != nil {
		writeTicketsError(w, http.StatusInternalServerError, "failed to update incident")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	hadManualAppend := false
	if req.AppendWorkNote != nil && strings.TrimSpace(*req.AppendWorkNote) != "" {
		hadManualAppend = true
		note := map[string]any{
			"ts":          time.Now().UTC().Format(time.RFC3339),
			"author_type": "operator",
			"kind":        "manual",
			"text":        strings.TrimSpace(*req.AppendWorkNote),
			"user_id":     p.UserID.String(),
		}
		blob, mErr := json.Marshal(note)
		if mErr != nil {
			writeTicketsError(w, http.StatusBadRequest, "invalid work note payload")
			return
		}
		tagN, execErr := tx.Exec(ctx, `
UPDATE incidents SET
  work_notes = COALESCE(work_notes, '[]'::jsonb) || jsonb_build_array($1::jsonb),
  updated_at = now()
WHERE id = $2
`, string(blob), id)
		if execErr != nil {
			writeTicketsError(w, http.StatusInternalServerError, "failed to append work note")
			return
		}
		if tagN.RowsAffected() == 0 {
			writeTicketsError(w, http.StatusNotFound, "incident not found")
			return
		}
	}

	setParts := make([]string, 0, 12)
	args := make([]any, 0, 14)
	ap := 1
	if req.ShortDescription != nil {
		setParts = append(setParts, fmt.Sprintf("short_description = $%d", ap))
		args = append(args, *req.ShortDescription)
		ap++
	}
	if req.State != nil {
		setParts = append(setParts, fmt.Sprintf("state = $%d", ap))
		args = append(args, *req.State)
		ap++
	}
	if req.Impact != nil {
		setParts = append(setParts, fmt.Sprintf("impact = $%d", ap))
		args = append(args, impactUrgencyOrDefault(req.Impact))
		ap++
	}
	if req.Urgency != nil {
		setParts = append(setParts, fmt.Sprintf("urgency = $%d", ap))
		args = append(args, impactUrgencyOrDefault(req.Urgency))
		ap++
	}
	if req.SLADueRFC3339 != nil {
		raw := strings.TrimSpace(*req.SLADueRFC3339)
		t, terr := time.Parse(time.RFC3339, raw)
		if terr != nil {
			writeTicketsError(w, http.StatusBadRequest, "sla_due must be RFC3339")
			return
		}
		setParts = append(setParts, fmt.Sprintf("sla_due = $%d", ap))
		args = append(args, t.UTC())
		ap++
	}
	if req.LinkedAssetHumanID != nil {
		hv := strings.TrimSpace(*req.LinkedAssetHumanID)
		if hv == "" {
			setParts = append(setParts, "cmdb_ci = NULL")
		} else {
			var did uuid.UUID
			if err := tx.QueryRow(ctx, `SELECT id FROM assets WHERE human_id = $1`, hv).Scan(&did); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					writeTicketsError(w, http.StatusBadRequest, "linked_asset_human_id not found")
					return
				}
				writeTicketsError(w, http.StatusInternalServerError, "failed to resolve asset")
				return
			}
			setParts = append(setParts, fmt.Sprintf("cmdb_ci = $%d", ap))
			args = append(args, did)
			ap++
		}
	}
	if req.ClearAssignedTo != nil && *req.ClearAssignedTo {
		setParts = append(setParts, "assigned_to = NULL")
	} else if req.AssignedToUserID != nil {
		at := strings.TrimSpace(*req.AssignedToUserID)
		if at == "" {
			setParts = append(setParts, "assigned_to = NULL")
		} else {
			assignID, err := uuid.Parse(at)
			if err != nil {
				writeTicketsError(w, http.StatusBadRequest, "invalid assigned_to_user_id")
				return
			}
			var exists bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id = $1)`, assignID).Scan(&exists); err != nil || !exists {
				writeTicketsError(w, http.StatusBadRequest, "assigned_to_user_id not found")
				return
			}
			setParts = append(setParts, fmt.Sprintf("assigned_to = $%d", ap))
			args = append(args, assignID)
			ap++
		}
	}

	if len(setParts) > 0 {
		setParts = append(setParts, "updated_at = now()")
		args = append(args, id)
		q := fmt.Sprintf(`UPDATE incidents SET %s WHERE id = $%d`, strings.Join(setParts, ", "), ap)
		tag, err := tx.Exec(ctx, q, args...)
		if err != nil {
			h.deps.Logger.Error("patch incident", "err", err)
			writeTicketsError(w, http.StatusInternalServerError, "failed to update incident")
			return
		}
		if tag.RowsAffected() == 0 {
			writeTicketsError(w, http.StatusNotFound, "incident not found")
			return
		}
	} else if len(setParts) == 0 && !hadManualAppend {
		writeTicketsError(w, http.StatusBadRequest, "no fields to update")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		writeTicketsError(w, http.StatusInternalServerError, "failed to update incident")
		return
	}
	h.notifyMutated()
	out, err := h.loadIncidentDetail(context.Background(), id)
	if err != nil {
		writeTicketsJSON(w, http.StatusOK, map[string]string{"status": "updated"})
		return
	}
	writeTicketsJSON(w, http.StatusOK, out)
}

func (h *incidentsHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !h.authorizeOperator(w, r) {
		return
	}
	id, err := parseIncidentUUID(r)
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid incident id")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	tag, err := h.deps.Pool.Exec(ctx, `DELETE FROM incidents WHERE id = $1`, id)
	if err != nil {
		writeTicketsError(w, http.StatusInternalServerError, "failed to delete incident")
		return
	}
	if tag.RowsAffected() == 0 {
		writeTicketsError(w, http.StatusNotFound, "incident not found")
		return
	}
	h.notifyMutated()
	w.WriteHeader(http.StatusNoContent)
}

func (h *incidentsHandler) handlePostResolution(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !h.authorizeOperator(w, r) {
		return
	}
	id, err := parseIncidentUUID(r)
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid incident id")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxIncidentJSONBody))
	_ = r.Body.Close()
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "could not read request body")
		return
	}
	var req createResolutionRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.Summary = strings.TrimSpace(req.Summary)
	if req.Summary == "" {
		writeTicketsError(w, http.StatusBadRequest, "summary is required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	var exists bool
	if err := h.deps.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM incidents WHERE id = $1)`, id).Scan(&exists); err != nil || !exists {
		writeTicketsError(w, http.StatusNotFound, "incident not found")
		return
	}
	var resID uuid.UUID
	if err := h.deps.Pool.QueryRow(ctx, `
INSERT INTO resolutions (incident_id, summary, markdown, details)
VALUES ($1, $2, $3, '{}'::jsonb)
RETURNING id
`, id, req.Summary, req.Markdown).Scan(&resID); err != nil {
		writeTicketsError(w, http.StatusInternalServerError, "failed to create resolution")
		return
	}
	h.notifyMutated()
	writeTicketsJSON(w, http.StatusCreated, map[string]any{"id": resID})
}

func (h *incidentsHandler) handlePostIncidentCommand(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	principal, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleOperator)
	if !ok {
		return
	}
	if h.deps.C2Hub == nil {
		writeTicketsError(w, http.StatusServiceUnavailable, "c2 hub not configured")
		return
	}
	incidentID, err := parseIncidentUUID(r)
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid incident id")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxDeviceCommandPayloadBytes+4096))
	_ = r.Body.Close()
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var req createIncidentCommandBody
	if err := json.Unmarshal(body, &req); err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	commandType := strings.TrimSpace(strings.ToLower(req.CommandType))
	if commandType == "" {
		writeTicketsError(w, http.StatusBadRequest, "command_type is required")
		return
	}
	payload := req.Payload

	ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
	defer cancel()

	var cmdbPtr *uuid.UUID
	qerr := h.deps.Pool.QueryRow(ctx, `
SELECT cmdb_ci FROM incidents WHERE id = $1 LIMIT 1
`, incidentID).Scan(&cmdbPtr)
	if errors.Is(qerr, pgx.ErrNoRows) || qerr != nil {
		writeTicketsError(w, http.StatusNotFound, "incident not found")
		return
	}
	if cmdbPtr == nil || *cmdbPtr == uuid.Nil {
		writeTicketsError(w, http.StatusConflict, "incident must be linked to a CMDB CI before running C2")
		return
	}

	deviceID := *cmdbPtr
	cmd, err := database.InsertDeviceCommand(ctx, h.deps.Pool, deviceID, commandType, payload, &incidentID)
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, err.Error())
		return
	}

	certSerial, err := ResolveAssetCertSerial(ctx, h.deps.Pool, deviceID)
	if err != nil {
		_ = database.FailDeviceCommandIfPending(ctx, h.deps.Pool, cmd.ID, "asset certificate not available: "+err.Error())
		writeTicketsError(w, http.StatusConflict, err.Error())
		return
	}

	dispatch := func() bool {
		return h.deps.C2Hub.DispatchJSON(certSerial, map[string]string{
			"action":       "device_command",
			"command_id":   cmd.ID.String(),
			"command_type": commandType,
			"payload":      payload,
		})
	}
	if !dispatch() {
		failCtx, failCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer failCancel()
		_, _ = database.FailDeviceCommand(failCtx, h.deps.Pool, cmd.ID, "agent not connected")
		writeTicketsError(w, http.StatusConflict, "agent not connected")
		return
	}

	if err := database.MarkDeviceCommandSent(ctx, h.deps.Pool, cmd.ID); err != nil && h.deps.Logger != nil {
		h.deps.Logger.Warn("mark device command sent failed", slog.String("command_id", cmd.ID.String()))
	}
	cmd.Status = models.DeviceCommandStatusSent

	auditCtx, auditCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer auditCancel()
	_ = auth.InsertAuditRecord(auditCtx, h.deps.Pool, auth.AuditRecord{
		UserID:        principal.UserID,
		Action:        "incident_command_executed",
		ResourceType:  "incident",
		ResourceID:    &incidentID,
		TargetAssetID: &deviceID,
		Details: map[string]any{
			"command_id":   cmd.ID.String(),
			"command_type": commandType,
			"channel":      "incident_console",
		},
		IPAddress: auth.ClientIP(r),
	})

	writeTicketsJSON(w, http.StatusCreated, cmd)
}

func pointerStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
