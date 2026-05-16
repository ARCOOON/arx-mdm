package database

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ARCOOON/arx-mdm/internal/models"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	maxDeviceCommandOutputLen = 256 * 1024
)

// InsertDeviceCommand creates a pending command row for the given asset (device).
func InsertDeviceCommand(ctx context.Context, pool *pgxpool.Pool, deviceID uuid.UUID, commandType, payload string) (models.DeviceCommand, error) {
	if pool == nil {
		return models.DeviceCommand{}, errors.New("database: pool is required")
	}
	commandType = strings.TrimSpace(strings.ToLower(commandType))
	if err := validateDeviceCommandType(commandType); err != nil {
		return models.DeviceCommand{}, err
	}
	payload = strings.TrimSpace(payload)
	if commandType != models.DeviceCommandTypeScript && payload != "" {
		return models.DeviceCommand{}, fmt.Errorf("database: payload only allowed for script commands")
	}

	var cmd models.DeviceCommand
	err := pool.QueryRow(ctx, `
INSERT INTO device_commands (device_id, command_type, payload, status)
VALUES ($1, $2, $3, $4)
RETURNING id, device_id, command_type, payload, status, output, created_at, completed_at
`, deviceID, commandType, payload, models.DeviceCommandStatusPending).Scan(
		&cmd.ID, &cmd.DeviceID, &cmd.CommandType, &cmd.Payload, &cmd.Status, &cmd.Output, &cmd.CreatedAt, &cmd.CompletedAt,
	)
	if err != nil {
		return models.DeviceCommand{}, fmt.Errorf("database: insert device command: %w", err)
	}
	return cmd, nil
}

// GetDeviceCommand returns one command by id, scoped to device_id when deviceID is not Nil.
func GetDeviceCommand(ctx context.Context, pool *pgxpool.Pool, commandID, deviceID uuid.UUID) (models.DeviceCommand, error) {
	if pool == nil {
		return models.DeviceCommand{}, errors.New("database: pool is required")
	}
	q := `
SELECT id, device_id, command_type, payload, status, output, created_at, completed_at
FROM device_commands
WHERE id = $1`
	args := []any{commandID}
	if deviceID != uuid.Nil {
		q += ` AND device_id = $2`
		args = append(args, deviceID)
	}
	var cmd models.DeviceCommand
	err := pool.QueryRow(ctx, q, args...).Scan(
		&cmd.ID, &cmd.DeviceID, &cmd.CommandType, &cmd.Payload, &cmd.Status, &cmd.Output, &cmd.CreatedAt, &cmd.CompletedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return models.DeviceCommand{}, fmt.Errorf("database: device command not found")
		}
		return models.DeviceCommand{}, fmt.Errorf("database: get device command: %w", err)
	}
	return cmd, nil
}

// ListDeviceCommands returns recent commands for an asset, newest first.
func ListDeviceCommands(ctx context.Context, pool *pgxpool.Pool, deviceID uuid.UUID, limit int) ([]models.DeviceCommand, error) {
	if pool == nil {
		return nil, errors.New("database: pool is required")
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	rows, err := pool.Query(ctx, `
SELECT id, device_id, command_type, payload, status, output, created_at, completed_at
FROM device_commands
WHERE device_id = $1
ORDER BY created_at DESC
LIMIT $2
`, deviceID, limit)
	if err != nil {
		return nil, fmt.Errorf("database: list device commands: %w", err)
	}
	defer rows.Close()

	var out []models.DeviceCommand
	for rows.Next() {
		var cmd models.DeviceCommand
		if err := rows.Scan(
			&cmd.ID, &cmd.DeviceID, &cmd.CommandType, &cmd.Payload, &cmd.Status, &cmd.Output, &cmd.CreatedAt, &cmd.CompletedAt,
		); err != nil {
			return nil, fmt.Errorf("database: scan device command: %w", err)
		}
		out = append(out, cmd)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("database: list device commands rows: %w", err)
	}
	return out, nil
}

// MarkDeviceCommandSent sets status to sent after the command was pushed to the agent.
func MarkDeviceCommandSent(ctx context.Context, pool *pgxpool.Pool, commandID uuid.UUID) error {
	return updateDeviceCommandStatus(ctx, pool, commandID, models.DeviceCommandStatusSent, "", false)
}

// DeviceCommandOwnedByCertSerial returns true when the command belongs to the asset for certSerial.
func DeviceCommandOwnedByCertSerial(ctx context.Context, pool *pgxpool.Pool, commandID uuid.UUID, certSerial string) (bool, error) {
	if pool == nil {
		return false, errors.New("database: pool is required")
	}
	certSerial = strings.TrimSpace(certSerial)
	var ok bool
	err := pool.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1
  FROM device_commands d
  JOIN assets a ON a.id = d.device_id
  WHERE d.id = $1 AND a.cert_serial = $2
)
`, commandID, certSerial).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("database: verify device command ownership: %w", err)
	}
	return ok, nil
}

// CompleteDeviceCommand records successful execution output.
func CompleteDeviceCommand(ctx context.Context, pool *pgxpool.Pool, commandID uuid.UUID, output string) (models.DeviceCommand, error) {
	output = truncateOutput(output)
	if err := updateDeviceCommandStatus(ctx, pool, commandID, models.DeviceCommandStatusCompleted, output, true); err != nil {
		return models.DeviceCommand{}, err
	}
	return GetDeviceCommand(ctx, pool, commandID, uuid.Nil)
}

// FailDeviceCommand records a failed execution with output or error text.
func FailDeviceCommand(ctx context.Context, pool *pgxpool.Pool, commandID uuid.UUID, output string) (models.DeviceCommand, error) {
	output = truncateOutput(output)
	if err := updateDeviceCommandStatus(ctx, pool, commandID, models.DeviceCommandStatusFailed, output, true); err != nil {
		return models.DeviceCommand{}, err
	}
	return GetDeviceCommand(ctx, pool, commandID, uuid.Nil)
}

// FailDeviceCommandIfPending marks a never-dispatched command as failed (e.g. agent offline).
func FailDeviceCommandIfPending(ctx context.Context, pool *pgxpool.Pool, commandID uuid.UUID, message string) error {
	message = truncateOutput(message)
	tag, err := pool.Exec(ctx, `
UPDATE device_commands
SET status = $2,
    output = $3,
    completed_at = now()
WHERE id = $1 AND status = $4
`, commandID, models.DeviceCommandStatusFailed, message, models.DeviceCommandStatusPending)
	if err != nil {
		return fmt.Errorf("database: fail pending device command: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("database: command not in pending state")
	}
	return nil
}

func updateDeviceCommandStatus(ctx context.Context, pool *pgxpool.Pool, commandID uuid.UUID, status, output string, setCompleted bool) error {
	if pool == nil {
		return errors.New("database: pool is required")
	}
	if err := validateDeviceCommandStatus(status); err != nil {
		return err
	}
	var err error
	if setCompleted {
		_, err = pool.Exec(ctx, `
UPDATE device_commands
SET status = $2,
    output = $3,
    completed_at = now()
WHERE id = $1
`, commandID, status, output)
	} else {
		_, err = pool.Exec(ctx, `
UPDATE device_commands
SET status = $2,
    output = $3
WHERE id = $1
`, commandID, status, output)
	}
	if err != nil {
		return fmt.Errorf("database: update device command: %w", err)
	}
	return nil
}

func validateDeviceCommandType(t string) error {
	switch t {
	case models.DeviceCommandTypePing, models.DeviceCommandTypeReboot, models.DeviceCommandTypeScript:
		return nil
	default:
		return fmt.Errorf("database: invalid command_type %q", t)
	}
}

func validateDeviceCommandStatus(s string) error {
	switch s {
	case models.DeviceCommandStatusPending, models.DeviceCommandStatusSent,
		models.DeviceCommandStatusCompleted, models.DeviceCommandStatusFailed:
		return nil
	default:
		return fmt.Errorf("database: invalid status %q", s)
	}
}

func truncateOutput(s string) string {
	if len(s) <= maxDeviceCommandOutputLen {
		return s
	}
	return s[:maxDeviceCommandOutputLen] + "\n…(output truncated)"
}
