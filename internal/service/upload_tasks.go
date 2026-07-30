package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ei4869/SharePipe-OSS/internal/core"
	"github.com/ei4869/SharePipe-OSS/internal/security"
	"github.com/ei4869/SharePipe-OSS/internal/storage"
)

func (s *Service) UploadTask(ctx context.Context, shareID, managementToken string) (core.UploadTask, error) {
	var task core.UploadTask
	var updated int64
	err := s.db.QueryRowContext(ctx, `SELECT t.share_id,t.status,t.updated_at
		FROM upload_tasks t JOIN shares sh ON sh.id=t.share_id
		WHERE t.share_id=? AND sh.manage_token_hash=?`, shareID, security.HashToken(managementToken)).
		Scan(&task.ShareID, &task.Status, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return core.UploadTask{}, core.ErrUnauthorized
	}
	if err != nil {
		return core.UploadTask{}, err
	}
	task.UpdatedAt = time.Unix(updated, 0).UTC()
	return task, nil
}

func (s *Service) PauseUploadTask(ctx context.Context, shareID, managementToken string) (core.UploadTask, error) {
	task, err := s.UploadTask(ctx, shareID, managementToken)
	if err != nil {
		return core.UploadTask{}, err
	}
	if task.Status == "paused" {
		return task, nil
	}
	if task.Status != "uploading" {
		return core.UploadTask{}, core.ErrConflict
	}
	now := s.now().Unix()
	result, err := s.db.ExecContext(ctx, `UPDATE upload_tasks SET status='paused',updated_at=?
		WHERE share_id=? AND status='uploading'
		  AND EXISTS(SELECT 1 FROM shares WHERE id=? AND status='uploading')`, now, shareID, shareID)
	if err != nil {
		return core.UploadTask{}, err
	}
	if changed, _ := result.RowsAffected(); changed == 0 {
		return core.UploadTask{}, core.ErrConflict
	}
	return s.UploadTask(ctx, shareID, managementToken)
}

func (s *Service) ResumeUploadTask(ctx context.Context, shareID, managementToken string) (core.UploadTask, error) {
	task, err := s.UploadTask(ctx, shareID, managementToken)
	if err != nil {
		return core.UploadTask{}, err
	}
	if task.Status == "uploading" {
		return task, nil
	}
	if task.Status != "paused" {
		return core.UploadTask{}, core.ErrConflict
	}
	now := s.now().UTC()
	var shareExpires int64
	if err := s.db.QueryRowContext(ctx, "SELECT expires_at FROM shares WHERE id=? AND status='uploading'", shareID).Scan(&shareExpires); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return core.UploadTask{}, core.ErrExpired
		}
		return core.UploadTask{}, err
	}
	extendedUntil := now.Add(time.Duration(s.cfg.Uploads.SessionTTLMinutes) * time.Minute)
	if shareTime := time.Unix(shareExpires, 0); extendedUntil.After(shareTime) {
		extendedUntil = shareTime
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return core.UploadTask{}, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, "UPDATE upload_tasks SET status='uploading',completion_session_id=NULL,updated_at=? WHERE share_id=? AND status='paused'", now.Unix(), shareID)
	if err != nil {
		return core.UploadTask{}, err
	}
	if changed, _ := result.RowsAffected(); changed == 0 {
		return core.UploadTask{}, core.ErrConflict
	}
	if _, err := tx.ExecContext(ctx, `UPDATE upload_sessions SET status=CASE WHEN status='expired' THEN 'open' ELSE status END,
		expires_at=CASE WHEN expires_at<? THEN ? ELSE expires_at END,updated_at=?
		WHERE share_id=? AND status IN ('open','expired')`, extendedUntil.Unix(), extendedUntil.Unix(), now.Unix(), shareID); err != nil {
		return core.UploadTask{}, err
	}
	if err := tx.Commit(); err != nil {
		return core.UploadTask{}, err
	}
	return s.UploadTask(ctx, shareID, managementToken)
}

func (s *Service) CancelUploadTask(ctx context.Context, shareID, managementToken string) (core.UploadTask, error) {
	task, err := s.UploadTask(ctx, shareID, managementToken)
	if err != nil {
		return core.UploadTask{}, err
	}
	if task.Status == "cancel_requested" || task.Status == "aborted" {
		return task, nil
	}
	if task.Status == "completed" || task.Status == "expired" || task.Status == "completing" {
		return core.UploadTask{}, core.ErrConflict
	}
	now := s.now().Unix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return core.UploadTask{}, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE upload_tasks SET status='cancel_requested',cancel_requested_at=?,updated_at=?
		WHERE share_id=? AND status IN ('uploading','paused')`, now, now, shareID)
	if err != nil {
		return core.UploadTask{}, err
	}
	if changed, _ := result.RowsAffected(); changed == 0 {
		return core.UploadTask{}, core.ErrConflict
	}
	if _, err := tx.ExecContext(ctx, `UPDATE shares SET status='revoked',revoked_at=?,updated_at=?
		WHERE id=? AND status='uploading'`, now, now, shareID); err != nil {
		return core.UploadTask{}, err
	}
	if err := tx.Commit(); err != nil {
		return core.UploadTask{}, err
	}
	if err := s.abortUploadSessions(ctx, shareID); err != nil {
		_ = s.markCleanupFailure(ctx, shareID, err)
		return s.UploadTask(ctx, shareID, managementToken)
	}
	_, _ = s.db.ExecContext(ctx, "UPDATE upload_tasks SET status='aborted',updated_at=? WHERE share_id=? AND status='cancel_requested'", s.now().Unix(), shareID)
	return s.UploadTask(ctx, shareID, managementToken)
}

func (s *Service) abortUploadSessions(ctx context.Context, shareID string) error {
	rows, err := s.db.QueryContext(ctx, `SELECT u.id,u.backend,u.storage_profile_id,COALESCE(u.s3_upload_id,''),e.storage_key
		FROM upload_sessions u JOIN entries e ON e.id=u.entry_id
		WHERE u.share_id=? AND u.status IN ('open','assembling','expired')`, shareID)
	if err != nil {
		return err
	}
	type pending struct{ id, backend, profileID, uploadID, key string }
	var sessions []pending
	for rows.Next() {
		var item pending
		if err := rows.Scan(&item.id, &item.backend, &item.profileID, &item.uploadID, &item.key); err != nil {
			rows.Close()
			return err
		}
		sessions = append(sessions, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	var cleanupErrors []error
	for _, item := range sessions {
		if item.backend == "s3" && item.uploadID != "" {
			store, openErr := s.storeFor(item.profileID, item.backend)
			if openErr != nil {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("open storage for upload session %s: %w", item.id, openErr))
			} else if multipart, ok := store.(storage.MultipartStore); !ok {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("storage for upload session %s does not support multipart abort", item.id))
			} else if abortErr := multipart.AbortMultipart(ctx, item.key, item.uploadID); abortErr != nil {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("abort upload session %s: %w", item.id, abortErr))
			}
		}
		if removeErr := os.RemoveAll(filepath.Join(s.stagingRoot, item.id)); removeErr != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("remove staging for upload session %s: %w", item.id, removeErr))
		}
	}
	if len(cleanupErrors) > 0 {
		return errors.Join(cleanupErrors...)
	}
	_, err = s.db.ExecContext(ctx, "UPDATE upload_sessions SET status='aborted',updated_at=? WHERE share_id=? AND status IN ('open','assembling','expired')", s.now().Unix(), shareID)
	return err
}
