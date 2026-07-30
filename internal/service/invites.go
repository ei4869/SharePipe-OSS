package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ei4869/SharePipe-OSS/internal/core"
	"github.com/ei4869/SharePipe-OSS/internal/security"
)

func (s *Service) CreateInvite(ctx context.Context, label string, maxUses *int, expiresAt *time.Time) (core.Invite, error) {
	return s.CreateInviteWithKind(ctx, label, maxUses, expiresAt, "standard", "")
}

func (s *Service) CreateInviteWithKind(ctx context.Context, label string, maxUses *int, expiresAt *time.Time, kind, deviceName string) (core.Invite, error) {
	if kind == "" {
		kind = "standard"
	}
	if kind != "standard" && kind != "trusted_device" {
		return core.Invite{}, fmt.Errorf("%w: unsupported invite kind", core.ErrValidation)
	}
	deviceName = strings.TrimSpace(deviceName)
	if kind == "trusted_device" {
		if deviceName == "" || len([]rune(deviceName)) > 120 {
			return core.Invite{}, fmt.Errorf("%w: trusted invite needs a device name", core.ErrValidation)
		}
		if strings.TrimSpace(label) == "" {
			label = deviceName
		}
		one := 1
		maxUses = &one
	}
	if maxUses != nil && *maxUses < 1 {
		return core.Invite{}, fmt.Errorf("%w: maxUses must be positive", core.ErrValidation)
	}
	if expiresAt != nil && !expiresAt.After(s.now()) {
		return core.Invite{}, fmt.Errorf("%w: invite expiry must be in the future", core.ErrValidation)
	}
	id, _, err := security.RandomToken(16)
	if err != nil {
		return core.Invite{}, err
	}
	random, digest, err := security.RandomToken(18)
	if err != nil {
		return core.Invite{}, err
	}
	code := "SP-" + random
	digest = security.HashToken(code)
	now := s.now().UTC()
	var maxValue, expiryValue any
	if maxUses != nil {
		maxValue = *maxUses
	}
	if expiresAt != nil {
		expiryValue = expiresAt.Unix()
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO invite_codes(
		id, code_hash, label, max_uses, expires_at, created_at, kind, device_name
	) VALUES(?, ?, ?, ?, ?, ?, ?, ?)`, id, digest, label, maxValue, expiryValue, now.Unix(), kind, deviceName)
	if err != nil {
		return core.Invite{}, fmt.Errorf("create invite: %w", err)
	}
	return core.Invite{ID: id, Code: code, Label: label, MaxUses: maxUses, ExpiresAt: expiresAt, CreatedAt: now, Kind: kind, DeviceName: deviceName}, nil
}

func (s *Service) ListInvites(ctx context.Context) ([]core.Invite, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, label, max_uses, uses, expires_at, revoked_at, created_at, kind, device_name
		FROM invite_codes ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]core.Invite, 0)
	for rows.Next() {
		invite, err := scanInvite(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, invite)
	}
	return result, rows.Err()
}

func (s *Service) RevokeInvite(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, "UPDATE invite_codes SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL", s.now().Unix(), id)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return core.ErrNotFound
	}
	return nil
}

func consumeInvite(ctx context.Context, tx *sql.Tx, code string, now time.Time) (core.Invite, error) {
	if code == "" {
		return core.Invite{}, fmt.Errorf("%w: invitation code is required", core.ErrValidation)
	}
	var invite core.Invite
	var maxUses, expiresAt, revokedAt sql.NullInt64
	var createdAt int64
	err := tx.QueryRowContext(ctx, `UPDATE invite_codes SET uses = uses + 1
		WHERE code_hash = ?
		  AND revoked_at IS NULL
		  AND (expires_at IS NULL OR expires_at > ?)
		  AND (max_uses IS NULL OR uses < max_uses)
		RETURNING id,label,max_uses,uses,expires_at,revoked_at,created_at,kind,device_name`,
		security.HashToken(code), now.Unix()).Scan(&invite.ID, &invite.Label, &maxUses, &invite.Uses, &expiresAt, &revokedAt, &createdAt, &invite.Kind, &invite.DeviceName)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return core.Invite{}, core.ErrUnauthorized
		}
		return core.Invite{}, fmt.Errorf("consume invitation: %w", err)
	}
	invite.MaxUses = nullableInt(maxUses)
	invite.ExpiresAt = nullableTime(expiresAt)
	invite.RevokedAt = nullableTime(revokedAt)
	invite.CreatedAt = time.Unix(createdAt, 0).UTC()
	return invite, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanInvite(scanner rowScanner) (core.Invite, error) {
	var invite core.Invite
	var maxUses, expiresAt, revokedAt sql.NullInt64
	var createdAt int64
	if err := scanner.Scan(&invite.ID, &invite.Label, &maxUses, &invite.Uses, &expiresAt, &revokedAt, &createdAt, &invite.Kind, &invite.DeviceName); err != nil {
		return core.Invite{}, err
	}
	invite.MaxUses = nullableInt(maxUses)
	invite.ExpiresAt = nullableTime(expiresAt)
	invite.RevokedAt = nullableTime(revokedAt)
	invite.CreatedAt = time.Unix(createdAt, 0).UTC()
	return invite, nil
}

func (s *Service) ValidateInvite(ctx context.Context, code string) error {
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM invite_codes
		WHERE code_hash = ? AND revoked_at IS NULL
		  AND (expires_at IS NULL OR expires_at > ?)
		  AND (max_uses IS NULL OR uses < max_uses)`,
		security.HashToken(code), s.now().Unix()).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return core.ErrUnauthorized
	}
	return err
}
