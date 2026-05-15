package cli

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/ARCOOON/arx-mdm/internal/auth"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/term"
)

const (
	defaultSetupUsername = "admin"
	enrollmentTokenTTL   = 7 * 24 * time.Hour
	setupPasswordBytes   = 24
	enrollmentSecretLen  = 16
)

// RunSetup ensures at least one admin exists: if there are no admin-role users,
// it creates user "admin" with a random password and prints credentials to out.
func RunSetup(ctx context.Context, pool *pgxpool.Pool, out, errOut io.Writer) error {
	n, err := auth.CountAdmins(ctx, pool)
	if err != nil {
		return fmt.Errorf("count admins: %w", err)
	}
	if n > 0 {
		_, _ = fmt.Fprintln(errOut, "admin users already exist; nothing to do")
		return nil
	}

	var exists bool
	err = pool.QueryRow(ctx, `
SELECT EXISTS(SELECT 1 FROM users WHERE lower(username) = lower($1))
`, defaultSetupUsername).Scan(&exists)
	if err != nil {
		return fmt.Errorf("check username: %w", err)
	}
	if exists {
		return fmt.Errorf("user %q already exists but no admin role was found; resolve users table manually", defaultSetupUsername)
	}

	pass, err := randomPasswordHex()
	if err != nil {
		return err
	}
	hash, err := auth.HashPassword(pass)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	_, err = pool.Exec(ctx, `
INSERT INTO users (username, password_hash, role) VALUES ($1, $2, $3)
`, defaultSetupUsername, hash, auth.RoleAdmin)
	if err != nil {
		return fmt.Errorf("insert admin: %w", err)
	}

	_, _ = fmt.Fprintf(out, "Admin user created.\nusername: %s\npassword: %s\n", defaultSetupUsername, pass)
	return nil
}

// RunCreateEnrollmentToken inserts a new enrollment token and writes the presentation secret to out (one line, no extra logging).
func RunCreateEnrollmentToken(ctx context.Context, pool *pgxpool.Pool, out, errOut io.Writer) error {
	var raw [enrollmentSecretLen]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Errorf("random secret: %w", err)
	}
	secret := hex.EncodeToString(raw[:])
	tokenHash := auth.EnrollmentPresentationHash(secret)

	expires := time.Now().UTC().Add(enrollmentTokenTTL)
	var id string
	err := pool.QueryRow(ctx, `
INSERT INTO enrollment_tokens (token_hash, expires_at)
VALUES ($1, $2)
RETURNING id::text
`, tokenHash, expires).Scan(&id)
	if err != nil {
		return fmt.Errorf("insert enrollment token: %w", err)
	}
	_, _ = fmt.Fprintf(errOut, "enrollment token id: %s (expires %s)\n", id, expires.Format(time.RFC3339))
	_, _ = fmt.Fprintln(out, secret)
	return nil
}

func randomPasswordHex() (string, error) {
	var b [setupPasswordBytes]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("random password: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

func readPassword(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		b, err := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// RunCreateUserInteractive prompts for username, password (hidden when TTY), and role, then inserts the user.
func RunCreateUserInteractive(ctx context.Context, pool *pgxpool.Pool, errOut io.Writer) error {
	reader := bufio.NewReader(os.Stdin)
	fmt.Fprint(os.Stderr, "Username: ")
	username, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("read username: %w", err)
	}
	username = strings.TrimSpace(username)
	if username == "" {
		return fmt.Errorf("username is required")
	}

	var taken bool
	err = pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE lower(username) = lower($1))`, username).Scan(&taken)
	if err != nil {
		return fmt.Errorf("check username: %w", err)
	}
	if taken {
		return fmt.Errorf("user %q already exists", username)
	}

	fmt.Fprint(os.Stderr, "Role (admin, operator, viewer): ")
	roleLine, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("read role: %w", err)
	}
	role, ok := auth.ParseRole(strings.TrimSpace(roleLine))
	if !ok {
		return fmt.Errorf("invalid role %q", strings.TrimSpace(roleLine))
	}

	pass, err := readPassword("Password: ")
	if err != nil {
		return fmt.Errorf("read password: %w", err)
	}
	pass2, err := readPassword("Confirm password: ")
	if err != nil {
		return fmt.Errorf("read password confirmation: %w", err)
	}
	if pass != pass2 {
		return fmt.Errorf("passwords do not match")
	}

	hash, err := auth.HashPassword(pass)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	_, err = pool.Exec(ctx, `
INSERT INTO users (username, password_hash, role) VALUES ($1, $2, $3)
`, username, hash, role)
	if err != nil {
		return fmt.Errorf("insert user: %w", err)
	}
	_, _ = fmt.Fprintf(errOut, "created user %q with role %s\n", username, role)
	return nil
}

// RunResetPassword updates the password for an existing user (username match is case-insensitive).
func RunResetPassword(ctx context.Context, pool *pgxpool.Pool, username string, errOut io.Writer) error {
	u := strings.TrimSpace(username)
	if u == "" {
		return fmt.Errorf("username is required")
	}

	var id string
	err := pool.QueryRow(ctx, `SELECT id::text FROM users WHERE lower(username) = lower($1) LIMIT 1`, u).Scan(&id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("user %q not found", u)
		}
		return fmt.Errorf("lookup user: %w", err)
	}

	pass, err := readPassword("New password: ")
	if err != nil {
		return fmt.Errorf("read password: %w", err)
	}
	pass2, err := readPassword("Confirm new password: ")
	if err != nil {
		return fmt.Errorf("read password confirmation: %w", err)
	}
	if pass != pass2 {
		return fmt.Errorf("passwords do not match")
	}

	hash, err := auth.HashPassword(pass)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	tag, err := pool.Exec(ctx, `UPDATE users SET password_hash = $1 WHERE id = $2::uuid`, hash, id)
	if err != nil {
		return fmt.Errorf("update password: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("no row updated")
	}
	_, _ = fmt.Fprintf(errOut, "password updated for user %q\n", u)
	return nil
}
