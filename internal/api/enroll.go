package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/ARCOOON/arx-mdm/internal/auth"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Android Enterprise / QR provisioning uses these Intent extra keys (see DevicePolicyManager).
const (
	androidExtraDeviceAdminComponent   = "android.app.extra.PROVISIONING_DEVICE_ADMIN_COMPONENT_NAME"
	androidExtraDeviceAdminSigChecksum = "android.app.extra.PROVISIONING_DEVICE_ADMIN_SIGNATURE_CHECKSUM"
	androidExtraAdminExtrasBundle      = "android.app.extra.PROVISIONING_ADMIN_EXTRAS_BUNDLE"
)

// Extras delivered to the DPC inside PROVISIONING_ADMIN_EXTRAS_BUNDLE (must match the Android app).
const (
	extraArxServerURL        = "com.arx.mdm.EXTRA_SERVER_URL"
	extraArxEnrollmentToken  = "com.arx.mdm.EXTRA_ENROLLMENT_TOKEN"
	defaultAndroidDPCPackage = "com.arx.mdm"
	defaultAndroidDPCClass   = "com.arx.mdm.ArxDeviceAdminReceiver"
)

const envMDMPublicBaseURL = "ARX_MDM_PUBLIC_BASE_URL"

// EnrollAndroidDeps wires dashboard-authenticated Android QR provisioning.
type EnrollAndroidDeps struct {
	Pool *pgxpool.Pool
	Auth DashboardAuth
}

type androidQRRequest struct {
	EnrollmentToken              string `json:"enrollment_token"`
	ServerURL                    string `json:"server_url"`
	DeviceAdminComponentName     string `json:"device_admin_component_name"`
	DeviceAdminSignatureChecksum string `json:"device_admin_signature_checksum"`
}

// RegisterEnrollAndroidRoutes registers POST /v1/enroll/android/qr.
func RegisterEnrollAndroidRoutes(mux *http.ServeMux, d EnrollAndroidDeps) {
	mux.HandleFunc("POST /v1/enroll/android/qr", handleAndroidEnrollmentQR(d))
}

// EnrollPublicDeps wires unauthenticated agent enrollment (CSR + presentation token).
type EnrollPublicDeps struct {
	Logger      *slog.Logger
	Coordinator *auth.EnrollmentCoordinator
}

type enrollAgentRequest struct {
	CSR   string `json:"csr"`
	Token string `json:"token"`
}

// RegisterPublicEnrollRoute registers POST /v1/enroll for agent bootstrap.
func RegisterPublicEnrollRoute(mux *http.ServeMux, d EnrollPublicDeps) {
	mux.HandleFunc("POST /v1/enroll", handlePublicEnroll(d))
}

func handlePublicEnroll(d EnrollPublicDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Logger == nil || d.Coordinator == nil {
			writeTicketsError(w, http.StatusInternalServerError, "enrollment is not configured")
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if cerr := r.Body.Close(); cerr != nil && err == nil {
			err = cerr
		}
		if err != nil {
			d.Logger.Error("read enroll body", "err", err, "request_id", r.Header.Get("X-Request-Id"))
			writeTicketsError(w, http.StatusBadRequest, "could not read request body")
			return
		}
		var req enrollAgentRequest
		if err := json.Unmarshal(body, &req); err != nil {
			writeTicketsError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if strings.TrimSpace(req.Token) == "" || strings.TrimSpace(req.CSR) == "" {
			writeTicketsError(w, http.StatusBadRequest, "csr and token are required")
			return
		}
		outcome, err := d.Coordinator.ProcessEnroll(r.Context(), req.Token, req.CSR)
		if err != nil {
			status, msg := auth.MapProcessEnrollHTTP(err)
			d.Logger.Warn("enroll failed",
				"status", status,
				"client_message", msg,
				"err", err,
				"request_id", r.Header.Get("X-Request-Id"),
			)
			writeTicketsError(w, status, msg)
			return
		}
		d.Logger.Info("enroll succeeded",
			"enrollment_token_id", outcome.EnrollmentTokenID.String(),
			"request_id", r.Header.Get("X-Request-Id"),
		)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		if err := json.NewEncoder(w).Encode(outcome.Response); err != nil {
			d.Logger.Error("encode enroll response", "err", err, "request_id", r.Header.Get("X-Request-Id"))
		}
	}
}

func handleAndroidEnrollmentQR(d EnrollAndroidDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if _, ok := d.Auth.RequireMinRole(w, r, auth.RoleOperator); !ok {
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
		if cerr := r.Body.Close(); cerr != nil && err == nil {
			err = cerr
		}
		if err != nil {
			writeTicketsError(w, http.StatusBadRequest, "could not read request body")
			return
		}
		var req androidQRRequest
		if err := json.Unmarshal(body, &req); err != nil {
			writeTicketsError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		token := strings.TrimSpace(req.EnrollmentToken)
		if token == "" {
			writeTicketsError(w, http.StatusBadRequest, "enrollment_token is required")
			return
		}
		hash := auth.EnrollmentPresentationHash(token)
		store := auth.NewPGXEnrollmentStore(d.Pool)
		valid, err := store.EnrollmentTokenUnusedValid(r.Context(), hash)
		if err != nil {
			writeTicketsError(w, http.StatusInternalServerError, "failed to validate enrollment token")
			return
		}
		if !valid {
			writeTicketsError(w, http.StatusBadRequest, "enrollment token is unknown, expired, or already used")
			return
		}
		serverURL := strings.TrimSpace(req.ServerURL)
		if serverURL == "" {
			serverURL = strings.TrimSpace(os.Getenv(envMDMPublicBaseURL))
		}
		if serverURL == "" {
			writeTicketsError(w, http.StatusBadRequest, "server_url is required (or set ARX_MDM_PUBLIC_BASE_URL)")
			return
		}
		comp := strings.TrimSpace(req.DeviceAdminComponentName)
		if comp == "" {
			comp = defaultAndroidDPCPackage + "/" + defaultAndroidDPCClass
		}
		sig := strings.TrimSpace(req.DeviceAdminSignatureChecksum)
		if sig == "" {
			writeTicketsError(w, http.StatusBadRequest, "device_admin_signature_checksum is required (SHA-256 of the DPC signing certificate per Android device owner provisioning)")
			return
		}
		bundle := map[string]string{
			extraArxServerURL:       serverURL,
			extraArxEnrollmentToken: token,
		}
		qrPayload := map[string]any{
			androidExtraDeviceAdminComponent:   comp,
			androidExtraDeviceAdminSigChecksum: sig,
			androidExtraAdminExtrasBundle:      bundle,
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(qrPayload)
	}
}
