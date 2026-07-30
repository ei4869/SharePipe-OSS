package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ei4869/SharePipe-OSS/internal/core"
	"github.com/ei4869/SharePipe-OSS/internal/security"
)

const TrustedDeviceCookieName = "sp_trusted_device"
const trustedDeviceTTL = 365 * 24 * time.Hour

type TrustedDeviceAuthorization struct {
	Device *core.TrustedDevice
	Renew  bool
}

func (s *Service) trustedIPDigest(ip string) string {
	if strings.TrimSpace(ip) == "" {
		return ""
	}
	mac := hmac.New(sha256.New, s.auditSalt)
	_, _ = mac.Write([]byte("trusted-device-ip:" + ip))
	return hex.EncodeToString(mac.Sum(nil))
}

func (s *Service) AuthorizeTrustedDevice(ctx context.Context, raw, remoteIP string) (TrustedDeviceAuthorization, error) {
	if raw == "" {
		return TrustedDeviceAuthorization{}, nil
	}
	var item core.TrustedDevice
	var expiresAt, lastSeen, revokedAt, createdAt int64
	err := s.db.QueryRowContext(ctx, `SELECT id,name,platform,user_agent,expires_at,last_seen_at,COALESCE(revoked_at,0),created_at
		FROM trusted_devices WHERE credential_hash=? AND revoked_at IS NULL AND expires_at>?`, security.HashToken(raw), s.now().Unix()).
		Scan(&item.ID, &item.Name, &item.Platform, &item.UserAgent, &expiresAt, &lastSeen, &revokedAt, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return TrustedDeviceAuthorization{}, nil
	}
	if err != nil {
		return TrustedDeviceAuthorization{}, err
	}
	item.ExpiresAt = time.Unix(expiresAt, 0).UTC()
	item.LastSeenAt = time.Unix(lastSeen, 0).UTC()
	item.CreatedAt = time.Unix(createdAt, 0).UTC()
	item.Scope = "local"
	now := s.now().UTC()
	renew := now.Sub(item.LastSeenAt) >= 24*time.Hour
	if renew {
		item.LastSeenAt = now
		item.ExpiresAt = now.Add(trustedDeviceTTL)
		_, err = s.db.ExecContext(ctx, "UPDATE trusted_devices SET last_seen_at=?,last_ip_digest=?,expires_at=?,updated_at=? WHERE id=? AND revoked_at IS NULL", now.Unix(), s.trustedIPDigest(remoteIP), item.ExpiresAt.Unix(), now.Unix(), item.ID)
		if err != nil {
			return TrustedDeviceAuthorization{}, err
		}
	}
	return TrustedDeviceAuthorization{Device: &item, Renew: renew}, nil
}

func (s *Service) ListTrustedDevices(ctx context.Context) ([]core.TrustedDevice, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,platform,user_agent,expires_at,last_seen_at,revoked_at,created_at FROM trusted_devices ORDER BY revoked_at IS NOT NULL,last_seen_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]core.TrustedDevice, 0)
	for rows.Next() {
		var item core.TrustedDevice
		var expires, lastSeen, created int64
		var revoked sql.NullInt64
		if err := rows.Scan(&item.ID, &item.Name, &item.Platform, &item.UserAgent, &expires, &lastSeen, &revoked, &created); err != nil {
			return nil, err
		}
		item.ExpiresAt = time.Unix(expires, 0).UTC()
		item.LastSeenAt = time.Unix(lastSeen, 0).UTC()
		item.CreatedAt = time.Unix(created, 0).UTC()
		item.RevokedAt = nullableTime(revoked)
		item.Scope = "local"
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Service) RenameTrustedDevice(ctx context.Context, id, name string) (core.TrustedDevice, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 120 {
		return core.TrustedDevice{}, fmt.Errorf("%w: invalid device name", core.ErrValidation)
	}
	result, err := s.db.ExecContext(ctx, "UPDATE trusted_devices SET name=?,updated_at=? WHERE id=?", name, s.now().Unix(), id)
	if err != nil {
		return core.TrustedDevice{}, err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return core.TrustedDevice{}, core.ErrNotFound
	}
	return core.TrustedDevice{ID: id, Name: name, Scope: "local"}, nil
}

func (s *Service) RevokeTrustedDevice(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, "UPDATE trusted_devices SET revoked_at=?,updated_at=? WHERE id=? AND revoked_at IS NULL", s.now().Unix(), s.now().Unix(), id)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return core.ErrNotFound
	}
	return nil
}

func newTrustedCredential() (string, string, error) { return security.RandomToken(32) }
