package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ClaimedEnrollmentToken is a row lock outcome: the presentation token was atomically consumed.
type ClaimedEnrollmentToken struct {
	ID      uuid.UUID
	AssetID *uuid.UUID
}

// PGXEnrollmentStore persists enrollment token state using PostgreSQL.
type PGXEnrollmentStore struct {
	pool *pgxpool.Pool
}

// NewPGXEnrollmentStore returns a store backed by the given pool. The pool must remain
// open for the lifetime of the store.
func NewPGXEnrollmentStore(pool *pgxpool.Pool) *PGXEnrollmentStore {
	return &PGXEnrollmentStore{pool: pool}
}

// EnrollmentTokenUnusedValid reports whether a presentation secret hash matches an unused,
// unexpired enrollment token row (does not consume the token).
func (s *PGXEnrollmentStore) EnrollmentTokenUnusedValid(ctx context.Context, presentationHash string) (bool, error) {
	if s == nil || s.pool == nil {
		return false, errors.New("auth: enrollment store is not initialized")
	}
	var ok bool
	err := s.pool.QueryRow(ctx, `
SELECT EXISTS(
  SELECT 1 FROM enrollment_tokens
  WHERE token_hash = $1 AND is_used = false AND expires_at > now()
)`, presentationHash).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("auth: check enrollment token: %w", err)
	}
	return ok, nil
}

// ClaimByPresentationHash atomically marks a valid token as used and returns its id and optional asset_id.
// If no matching unused, unexpired token exists, it returns ErrEnrollmentTokenInvalid.
func (s *PGXEnrollmentStore) ClaimByPresentationHash(ctx context.Context, presentationHash string) (*ClaimedEnrollmentToken, error) {
	if s == nil || s.pool == nil {
		return nil, errors.New("auth: enrollment store is not initialized")
	}
	const q = `
UPDATE enrollment_tokens
SET is_used = true, used_at = now()
WHERE token_hash = $1
  AND is_used = false
  AND expires_at > now()
RETURNING id, asset_id
`
	var out ClaimedEnrollmentToken
	err := s.pool.QueryRow(ctx, q, presentationHash).Scan(&out.ID, &out.AssetID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrEnrollmentTokenInvalid
		}
		return nil, fmt.Errorf("auth: claim enrollment token: %w", err)
	}
	return &out, nil
}

// ReleaseClaim resets is_used for a token that was claimed but could not complete enrollment
// (for example CA sign failure), allowing the same presentation secret to be retried.
func (s *PGXEnrollmentStore) ReleaseClaim(ctx context.Context, id uuid.UUID) error {
	if s == nil || s.pool == nil {
		return errors.New("auth: enrollment store is not initialized")
	}
	const q = `
UPDATE enrollment_tokens
SET is_used = false, used_at = NULL
WHERE id = $1 AND is_used = true
`
	tag, err := s.pool.Exec(ctx, q, id)
	if err != nil {
		return fmt.Errorf("auth: release enrollment token claim: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("auth: release enrollment token claim: expected 1 row updated, got %d", tag.RowsAffected())
	}
	return nil
}
