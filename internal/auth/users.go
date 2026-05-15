package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

// Dashboard role names (stored in users.role and JWT "role" claim).
const (
	RoleAdmin    = "admin"
	RoleOperator = "operator"
	RoleViewer   = "viewer"
)

// Principal is an authenticated dashboard user extracted from a JWT.
type Principal struct {
	UserID   uuid.UUID
	Username string
	Role     string
}

type ctxKey int

const principalCtxKey ctxKey = 1

// WithPrincipal attaches a dashboard principal to the context.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalCtxKey, p)
}

// PrincipalFromContext returns the principal if present.
func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	v := ctx.Value(principalCtxKey)
	if v == nil {
		return Principal{}, false
	}
	p, ok := v.(Principal)
	return p, ok
}

// ParseRole validates and normalizes a role string.
func ParseRole(s string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case RoleAdmin:
		return RoleAdmin, true
	case RoleOperator:
		return RoleOperator, true
	case RoleViewer:
		return RoleViewer, true
	default:
		return "", false
	}
}

// RoleRank is used for RBAC comparisons (higher includes lower).
func RoleRank(role string) int {
	r, ok := ParseRole(role)
	if !ok {
		return 0
	}
	switch r {
	case RoleAdmin:
		return 3
	case RoleOperator:
		return 2
	case RoleViewer:
		return 1
	default:
		return 0
	}
}

// HasAtLeast returns true if role meets or exceeds minRole (both must be valid roles).
func HasAtLeast(role, minRole string) bool {
	a := RoleRank(role)
	b := RoleRank(minRole)
	return a > 0 && b > 0 && a >= b
}

const bcryptCost = 12

// HashPassword returns a bcrypt hash suitable for users.password_hash.
func HashPassword(plain string) (string, error) {
	if strings.TrimSpace(plain) == "" {
		return "", errors.New("empty password")
	}
	b, err := bcrypt.GenerateFromPassword([]byte(plain), bcryptCost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// CheckPassword compares a bcrypt hash with a plaintext password.
func CheckPassword(hash, plain string) bool {
	if hash == "" || plain == "" {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}

type arxClaims struct {
	Username string `json:"username"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

// JWTService issues and verifies HS256 session tokens for the dashboard.
type JWTService struct {
	secret []byte
	issuer string
	ttl    time.Duration
}

// NewJWTService builds a JWT signer/verifier. secret must be sufficiently long for HS256 in production.
func NewJWTService(secret, issuer string, ttl time.Duration) (*JWTService, error) {
	secret = strings.TrimSpace(secret)
	if len(secret) < 32 {
		return nil, fmt.Errorf("jwt secret must be at least 32 bytes")
	}
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	iss := strings.TrimSpace(issuer)
	if iss == "" {
		iss = "arx-mdm"
	}
	return &JWTService{secret: []byte(secret), issuer: iss, ttl: ttl}, nil
}

// Issue returns a signed JWT and its expiry time.
func (j *JWTService) Issue(p Principal) (token string, exp time.Time, err error) {
	if _, ok := ParseRole(p.Role); !ok {
		return "", time.Time{}, fmt.Errorf("invalid role")
	}
	if p.UserID == uuid.Nil {
		return "", time.Time{}, fmt.Errorf("invalid user id")
	}
	now := time.Now().UTC()
	exp = now.Add(j.ttl)
	claims := arxClaims{
		Username: p.Username,
		Role:     p.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    j.issuer,
			Subject:   p.UserID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp),
			ID:        uuid.NewString(),
		},
	}
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	token, err = t.SignedString(j.secret)
	if err != nil {
		return "", time.Time{}, err
	}
	return token, exp, nil
}

// Parse validates a JWT string and returns the principal.
func (j *JWTService) Parse(tokenString string) (Principal, error) {
	var out Principal
	tok, err := jwt.ParseWithClaims(strings.TrimSpace(tokenString), &arxClaims{}, func(t *jwt.Token) (any, error) {
		if t.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return j.secret, nil
	})
	if err != nil || !tok.Valid {
		return out, fmt.Errorf("invalid token")
	}
	claims, ok := tok.Claims.(*arxClaims)
	if !ok {
		return out, fmt.Errorf("invalid claims")
	}
	uid, err := uuid.Parse(strings.TrimSpace(claims.Subject))
	if err != nil {
		return out, fmt.Errorf("invalid subject")
	}
	role, ok := ParseRole(claims.Role)
	if !ok {
		return out, fmt.Errorf("invalid role claim")
	}
	un := strings.TrimSpace(claims.Username)
	if un == "" {
		return out, fmt.Errorf("invalid username claim")
	}
	out.UserID = uid
	out.Username = un
	out.Role = role
	return out, nil
}

// GetUserByUsername loads a user row for login.
func GetUserByUsername(ctx context.Context, pool *pgxpool.Pool, username string) (id uuid.UUID, hash string, role string, err error) {
	u := strings.TrimSpace(username)
	if u == "" {
		return uuid.Nil, "", "", errors.New("empty username")
	}
	err = pool.QueryRow(ctx, `
SELECT id, password_hash, role FROM users WHERE lower(username) = lower($1) LIMIT 1
`, u).Scan(&id, &hash, &role)
	return id, hash, role, err
}

// CountUsers returns total users (for bootstrap gate).
func CountUsers(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	var n int64
	err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

// BootstrapAdminIfEmpty creates the first admin when the users table is empty and credentials are provided.
func BootstrapAdminIfEmpty(ctx context.Context, pool *pgxpool.Pool, username, password string) error {
	n, err := CountUsers(ctx, pool)
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	u := strings.TrimSpace(username)
	if u == "" || strings.TrimSpace(password) == "" {
		return nil
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `
INSERT INTO users (username, password_hash, role) VALUES ($1, $2, $3)
`, u, hash, RoleAdmin)
	return err
}

// CountAdmins returns users with admin role.
func CountAdmins(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	var n int64
	err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE role = $1`, RoleAdmin).Scan(&n)
	return n, err
}

// IsLastAdmin reports whether the given user id is the only admin account.
func IsLastAdmin(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID) (bool, error) {
	var role string
	err := pool.QueryRow(ctx, `SELECT role FROM users WHERE id = $1`, userID).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if role != RoleAdmin {
		return false, nil
	}
	n, err := CountAdmins(ctx, pool)
	if err != nil {
		return false, err
	}
	return n == 1, nil
}
