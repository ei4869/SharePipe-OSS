package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/ei4869/SharePipe-OSS/internal/core"
	"github.com/ei4869/SharePipe-OSS/internal/security"
)

func (s *Service) RecoverUpload(ctx context.Context, managementToken string) (core.ShareCreation, error) {
	share, _, err := s.findShare(ctx, "manage_token_hash", security.HashToken(managementToken))
	if err != nil {
		return core.ShareCreation{}, err
	}
	now := s.now().UTC()
	if !share.ExpiresAt.After(now) || (share.Status != "uploading" && share.Status != "active") {
		return core.ShareCreation{}, core.ErrExpired
	}
	var uploadTaskStatus string
	if err := s.db.QueryRowContext(ctx, "SELECT status FROM upload_tasks WHERE share_id=?", share.ID).Scan(&uploadTaskStatus); err != nil {
		return core.ShareCreation{}, err
	}
	if uploadTaskStatus == "cancel_requested" || uploadTaskStatus == "aborted" || uploadTaskStatus == "expired" {
		return core.ShareCreation{}, core.ErrExpired
	}
	if share.Status == "uploading" && uploadTaskStatus == "uploading" {
		extendedUntil := now.Add(time.Duration(s.cfg.Uploads.SessionTTLMinutes) * time.Minute)
		if extendedUntil.After(share.ExpiresAt) {
			extendedUntil = share.ExpiresAt
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE upload_sessions SET
			status=CASE WHEN status='expired' THEN 'open' ELSE status END,
			expires_at=CASE WHEN expires_at<? THEN ? ELSE expires_at END,updated_at=?
			WHERE share_id=? AND status IN ('open','expired')`,
			extendedUntil.Unix(), extendedUntil.Unix(), now.Unix(), share.ID); err != nil {
			return core.ShareCreation{}, err
		}
	}
	publicToken, err := s.recoverPublicToken(ctx, share.ID)
	if err != nil {
		return core.ShareCreation{}, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT u.id,u.entry_id,e.path,u.backend,u.chunk_size,u.expected_parts,
		u.expected_size,u.received_bytes,u.status FROM upload_sessions u JOIN entries e ON e.id=u.entry_id
		WHERE u.share_id=? ORDER BY e.path`, share.ID)
	if err != nil {
		return core.ShareCreation{}, err
	}
	defer rows.Close()
	uploads := make([]core.UploadDescriptor, 0)
	for rows.Next() {
		var upload core.UploadDescriptor
		if err := rows.Scan(&upload.SessionID, &upload.EntryID, &upload.Path, &upload.Backend,
			&upload.ChunkSize, &upload.PartCount, &upload.ExpectedSize, &upload.ReceivedBytes, &upload.Status); err != nil {
			return core.ShareCreation{}, err
		}
		upload.Token = managementToken
		upload.Parts, err = s.uploadParts(ctx, upload.SessionID)
		if err != nil {
			return core.ShareCreation{}, err
		}
		uploads = append(uploads, upload)
	}
	if err := rows.Err(); err != nil {
		return core.ShareCreation{}, err
	}
	base := strings.TrimRight(s.cfg.Server.PublicBaseURL, "/")
	return core.ShareCreation{
		Share: share, PublicToken: publicToken, ManagementToken: managementToken,
		PublicURL:     base + "/s/" + url.PathEscape(publicToken),
		ManagementURL: base + "/m/" + url.PathEscape(managementToken),
		RecoveryURL:   base + "/resume/" + url.PathEscape(managementToken), UploadTaskStatus: uploadTaskStatus, Uploads: uploads,
	}, nil
}

func (s *Service) recoverPublicToken(ctx context.Context, shareID string) (string, error) {
	var encrypted string
	if err := s.db.QueryRowContext(ctx, "SELECT public_token_encrypted FROM shares WHERE id=?", shareID).Scan(&encrypted); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", core.ErrNotFound
		}
		return "", err
	}
	if encrypted == "" {
		replacement, digest, err := security.RandomToken(32)
		if err != nil {
			return "", err
		}
		replacementEncrypted, err := security.EncryptSecret(s.storageMasterKey, []byte(replacement), "public-token:"+shareID)
		if err != nil {
			return "", err
		}
		result, err := s.db.ExecContext(ctx, `UPDATE shares SET public_token_hash=?,public_token_encrypted=?
			WHERE id=? AND public_token_encrypted=''`, digest, replacementEncrypted, shareID)
		if err != nil {
			return "", err
		}
		if changed, _ := result.RowsAffected(); changed > 0 {
			return replacement, nil
		}
		if err := s.db.QueryRowContext(ctx, "SELECT public_token_encrypted FROM shares WHERE id=?", shareID).Scan(&encrypted); err != nil {
			return "", err
		}
	}
	plain, err := security.DecryptSecret(s.storageMasterKey, encrypted, "public-token:"+shareID)
	if err != nil {
		return "", fmt.Errorf("decrypt public share token: %w", err)
	}
	return string(plain), nil
}
