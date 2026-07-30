package service

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"strings"
	"time"

	"github.com/ei4869/SharePipe-OSS/internal/core"
	"github.com/ei4869/SharePipe-OSS/internal/security"
)

func (s *Service) pairCodeDigest(code string) string {
	mac := hmac.New(sha256.New, s.storageMasterKey)
	_, _ = mac.Write([]byte("pair-code:" + code))
	return hex.EncodeToString(mac.Sum(nil))
}

func randomPairCode() (string, error) {
	value, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", value.Int64()), nil
}

func (s *Service) ActivatePairCode(ctx context.Context, shareID string) (core.PairingState, error) {
	var enabled bool
	var status string
	if err := s.db.QueryRowContext(ctx, "SELECT pairing_enabled,status FROM shares WHERE id=?", shareID).Scan(&enabled, &status); err != nil {
		return core.PairingState{}, err
	}
	if !enabled {
		return core.PairingState{Enabled: false}, nil
	}
	if status != "active" {
		return core.PairingState{}, core.ErrConflict
	}
	var encrypted string
	var expiry int64
	err := s.db.QueryRowContext(ctx, "SELECT code_encrypted,expires_at FROM pair_codes WHERE share_id=? AND revoked_at IS NULL AND expires_at>?", shareID, s.now().Unix()).Scan(&encrypted, &expiry)
	if err == nil {
		plain, e := security.DecryptSecret(s.storageMasterKey, encrypted, "pair-code:"+shareID)
		if e != nil {
			return core.PairingState{}, e
		}
		expires := time.Unix(expiry, 0).UTC()
		return core.PairingState{Enabled: true, Code: string(plain), ExpiresAt: &expires}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return core.PairingState{}, err
	}
	for attempt := 0; attempt < 12; attempt++ {
		code, e := randomPairCode()
		if e != nil {
			return core.PairingState{}, e
		}
		encrypted, e = security.EncryptSecret(s.storageMasterKey, []byte(code), "pair-code:"+shareID)
		if e != nil {
			return core.PairingState{}, e
		}
		now := s.now().UTC()
		expires := now.Add(time.Duration(s.RuntimeSettings().PairCodeTTLSeconds) * time.Second)
		_, e = s.db.ExecContext(ctx, `INSERT INTO pair_codes(share_id,code_digest,code_encrypted,expires_at,created_at,updated_at) VALUES(?,?,?,?,?,?)
			ON CONFLICT(share_id) DO UPDATE SET code_digest=excluded.code_digest,code_encrypted=excluded.code_encrypted,expires_at=excluded.expires_at,revoked_at=NULL,updated_at=excluded.updated_at`, shareID, s.pairCodeDigest(code), encrypted, expires.Unix(), now.Unix(), now.Unix())
		if e == nil {
			return core.PairingState{Enabled: true, Code: code, ExpiresAt: &expires}, nil
		}
	}
	return core.PairingState{}, core.ErrConflict
}

func (s *Service) PairingForManagedShare(ctx context.Context, shareID string) (core.PairingState, error) {
	var enabled bool
	if err := s.db.QueryRowContext(ctx, "SELECT pairing_enabled FROM shares WHERE id=?", shareID).Scan(&enabled); err != nil {
		return core.PairingState{}, err
	}
	if !enabled {
		return core.PairingState{Enabled: false}, nil
	}
	var encrypted string
	var expiry int64
	var revoked sql.NullInt64
	if err := s.db.QueryRowContext(ctx, "SELECT code_encrypted,expires_at,revoked_at FROM pair_codes WHERE share_id=?", shareID).Scan(&encrypted, &expiry, &revoked); errors.Is(err, sql.ErrNoRows) {
		return core.PairingState{Enabled: true}, nil
	} else if err != nil {
		return core.PairingState{}, err
	}
	if revoked.Valid {
		return core.PairingState{Enabled: true}, nil
	}
	plain, err := security.DecryptSecret(s.storageMasterKey, encrypted, "pair-code:"+shareID)
	if err != nil {
		return core.PairingState{Enabled: true, Expired: true}, nil
	}
	expires := time.Unix(expiry, 0).UTC()
	return core.PairingState{Enabled: true, Code: string(plain), ExpiresAt: &expires, Expired: !expires.After(s.now())}, nil
}

func (s *Service) EnsureManagedPairCode(ctx context.Context, manageToken string) (core.PairingState, error) {
	share, _, err := s.findShare(ctx, "manage_token_hash", security.HashToken(manageToken))
	if err != nil {
		return core.PairingState{}, err
	}
	if share.Status != "active" {
		return core.PairingState{}, core.ErrConflict
	}
	_, err = s.db.ExecContext(ctx, "UPDATE shares SET pairing_enabled=1,updated_at=? WHERE id=?", s.now().Unix(), share.ID)
	if err != nil {
		return core.PairingState{}, err
	}
	_, _ = s.db.ExecContext(ctx, "UPDATE pair_codes SET revoked_at=?,updated_at=? WHERE share_id=? AND revoked_at IS NULL", s.now().Unix(), s.now().Unix(), share.ID)
	return s.ActivatePairCode(ctx, share.ID)
}

func (s *Service) DisableManagedPairCode(ctx context.Context, manageToken string) error {
	share, _, err := s.findShare(ctx, "manage_token_hash", security.HashToken(manageToken))
	if err != nil {
		return err
	}
	now := s.now().Unix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "UPDATE shares SET pairing_enabled=0,updated_at=? WHERE id=?", now, share.ID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE pair_codes SET revoked_at=?,updated_at=? WHERE share_id=? AND revoked_at IS NULL", now, now, share.ID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) ResolvePairCode(ctx context.Context, code string) (string, time.Time, error) {
	if len(code) != 6 {
		return "", time.Time{}, core.ErrNotFound
	}
	for _, r := range code {
		if r < '0' || r > '9' {
			return "", time.Time{}, core.ErrNotFound
		}
	}
	var shareID string
	var expires int64
	err := s.db.QueryRowContext(ctx, `SELECT s.id,s.expires_at FROM pair_codes p JOIN shares s ON s.id=p.share_id WHERE p.code_digest=? AND p.revoked_at IS NULL AND p.expires_at>? AND s.status='active' AND s.expires_at>?`, s.pairCodeDigest(code), s.now().Unix(), s.now().Unix()).Scan(&shareID, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return "", time.Time{}, core.ErrNotFound
	}
	if err != nil {
		return "", time.Time{}, err
	}
	raw, digest, err := security.RandomToken(32)
	if err != nil {
		return "", time.Time{}, err
	}
	id, _, err := security.RandomToken(16)
	if err != nil {
		return "", time.Time{}, err
	}
	now := s.now().Unix()
	if _, err = s.db.ExecContext(ctx, "INSERT INTO share_access_tokens(id,share_id,token_hash,source,expires_at,last_used_at,created_at) VALUES(?,?,?,'pairing',?,?,?)", id, shareID, digest, expires, now, now); err != nil {
		return "", time.Time{}, err
	}
	base := s.cfg.Server.PublicBaseURL
	return strings.TrimRight(base, "/") + "/s/" + url.PathEscape(raw), time.Unix(expires, 0).UTC(), nil
}
