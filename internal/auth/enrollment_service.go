package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"arx-mdm/internal/pki"

	"github.com/google/uuid"
)

// EnrollmentTokenStore abstracts persistence for enrollment token lifecycle.
type EnrollmentTokenStore interface {
	ClaimByPresentationHash(ctx context.Context, presentationHash string) (*ClaimedEnrollmentToken, error)
	ReleaseClaim(ctx context.Context, id uuid.UUID) error
}

// EnrollmentCoordinator orchestrates database-backed enrollment and embedded CA signing.
type EnrollmentCoordinator struct {
	Store EnrollmentTokenStore
	PKI   *pki.Authority
}

// NewEnrollmentCoordinator returns a coordinator with the given dependencies.
func NewEnrollmentCoordinator(store EnrollmentTokenStore, pkiAuth *pki.Authority) *EnrollmentCoordinator {
	return &EnrollmentCoordinator{Store: store, PKI: pkiAuth}
}

// EnrollOutcome is the successful result of enrollment including the consumed DB row id for audit logs.
type EnrollOutcome struct {
	Response          EnrollAPIResponse
	EnrollmentTokenID uuid.UUID
}

// ProcessEnroll verifies the CSR, atomically consumes a valid enrollment token, signs with the
// embedded CA, and returns PEM material for the agent. If signing or response building fails after
// a successful claim, the claim is released so the client may retry with the same presentation secret.
func (c *EnrollmentCoordinator) ProcessEnroll(ctx context.Context, presentationSecret, csrPEM string) (EnrollOutcome, error) {
	var zero EnrollOutcome
	if c == nil || c.Store == nil || c.PKI == nil {
		return zero, errors.New("auth: enrollment coordinator is not fully initialized")
	}
	secret := strings.TrimSpace(presentationSecret)
	csr := strings.TrimSpace(csrPEM)
	if secret == "" || csr == "" {
		return zero, errors.New("auth: presentation token and CSR are required")
	}

	if err := ValidateCSRPEM([]byte(csr)); err != nil {
		return zero, err
	}

	cr, err := ParseCertificateRequestPEM([]byte(csr))
	if err != nil {
		return zero, fmt.Errorf("%w: %v", ErrEnrollmentCSRInvalid, err)
	}
	hostname := HostnameFromCSR(cr)
	if hostname == "" {
		return zero, fmt.Errorf("%w: missing hostname (CN or DNS SAN)", ErrEnrollmentCSRInvalid)
	}

	hash := EnrollmentPresentationHash(secret)
	claimed, err := c.Store.ClaimByPresentationHash(ctx, hash)
	if err != nil {
		return zero, err
	}

	assetID := uuid.Nil
	if claimed.AssetID != nil {
		assetID = *claimed.AssetID
	}

	signed, err := c.PKI.IssueClientCertificate(ctx, assetID, hostname, cr)
	if err != nil {
		if relErr := c.Store.ReleaseClaim(ctx, claimed.ID); relErr != nil {
			return zero, fmt.Errorf("auth: embedded ca sign failed: %w (release claim: %v)", err, relErr)
		}
		return zero, fmt.Errorf("auth: embedded ca sign failed: %w", err)
	}

	resp, err := BuildEnrollAPIResponse(signed)
	if err != nil {
		if relErr := c.Store.ReleaseClaim(ctx, claimed.ID); relErr != nil {
			return zero, fmt.Errorf("auth: build enroll response: %w (release claim: %v)", err, relErr)
		}
		return zero, fmt.Errorf("auth: build enroll response: %w", err)
	}
	return EnrollOutcome{
		Response:          resp,
		EnrollmentTokenID: claimed.ID,
	}, nil
}

// MapProcessEnrollHTTP maps errors from ProcessEnroll to an HTTP status and a client-safe message.
func MapProcessEnrollHTTP(err error) (status int, clientMsg string) {
	if err == nil {
		return http.StatusOK, ""
	}
	if errors.Is(err, ErrEnrollmentTokenInvalid) {
		return http.StatusUnauthorized, "invalid or expired enrollment token"
	}
	if errors.Is(err, ErrEnrollmentCSRInvalid) {
		return http.StatusBadRequest, "invalid certificate signing request"
	}
	if errors.Is(err, ErrEnrollmentSignEmpty) {
		return http.StatusBadGateway, "certificate authority returned an empty response"
	}
	return http.StatusBadGateway, "enrollment failed"
}
