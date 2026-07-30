package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ei4869/SharePipe-OSS/internal/storage"
)

func (s *Service) RunBackground(ctx context.Context) {
	jobTicker := time.NewTicker(10 * time.Second)
	maintenanceTicker := time.NewTicker(time.Minute)
	backupTicker := time.NewTicker(30 * time.Minute)
	defer jobTicker.Stop()
	defer maintenanceTicker.Stop()
	defer backupTicker.Stop()
	_ = s.RunMaintenanceOnce(ctx)
	_ = s.RunBackupIfDue(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-jobTicker.C:
			_ = s.RunOneJob(ctx)
		case <-maintenanceTicker.C:
			_ = s.RunMaintenanceOnce(ctx)
		case <-backupTicker.C:
			_ = s.RunBackupIfDue(ctx)
		}
	}
}

func (s *Service) RunOneJob(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := s.now()
	var id, kind, payload string
	var attempts int
	err = tx.QueryRowContext(ctx, `SELECT id, kind, payload, attempts FROM jobs
		WHERE state='queued' AND run_at<=? AND (lease_until IS NULL OR lease_until<?)
		ORDER BY run_at, created_at LIMIT 1`, now.Unix(), now.Unix()).Scan(&id, &kind, &payload, &attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE jobs SET state='running', lease_until=?, attempts=attempts+1,
		updated_at=? WHERE id=? AND state='queued'`, now.Add(5*time.Minute).Unix(), now.Unix(), id)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return nil
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	runErr := s.executeJob(ctx, kind, payload)
	if runErr == nil {
		_, err = s.db.ExecContext(ctx, `UPDATE jobs SET state='succeeded', lease_until=NULL,
			last_error=NULL, updated_at=? WHERE id=?`, s.now().Unix(), id)
		return err
	}
	if attempts+1 >= 5 {
		_, _ = s.db.ExecContext(ctx, `UPDATE jobs SET state='failed', lease_until=NULL,
			last_error=?, updated_at=? WHERE id=?`, runErr.Error(), s.now().Unix(), id)
		return runErr
	}
	delay := time.Duration(1<<attempts) * time.Minute
	_, _ = s.db.ExecContext(ctx, `UPDATE jobs SET state='queued', lease_until=NULL, last_error=?,
		run_at=?, updated_at=? WHERE id=?`, runErr.Error(), s.now().Add(delay).Unix(), s.now().Unix(), id)
	return runErr
}

func (s *Service) executeJob(ctx context.Context, kind, payload string) error {
	switch kind {
	case "build_zip":
		var data struct {
			ShareID string `json:"shareId"`
		}
		if err := json.Unmarshal([]byte(payload), &data); err != nil || data.ShareID == "" {
			return errors.New("invalid build_zip payload")
		}
		return s.BuildCachedZip(ctx, data.ShareID)
	default:
		return fmt.Errorf("unsupported job kind: %s", kind)
	}
}

func (s *Service) RunMaintenanceOnce(ctx context.Context) error {
	now := s.now().UTC()
	if _, err := s.db.ExecContext(ctx, `UPDATE shares SET status='expired', updated_at=?
		WHERE status IN ('active','uploading') AND expires_at<=?`, now.Unix(), now.Unix()); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE upload_tasks SET status='expired',updated_at=?
		WHERE status IN ('uploading','paused') AND share_id IN (SELECT id FROM shares WHERE status='expired')`, now.Unix()); err != nil {
		return err
	}
	if err := s.expireUploadSessions(ctx, now); err != nil {
		return err
	}
	if err := s.cleanupShareContent(ctx, now); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM shares
		WHERE status='metadata_only' AND metadata_delete_after<=?`, now.Unix()); err != nil {
		return err
	}
	if err := s.cleanupExpiredZip(ctx); err != nil {
		return err
	}
	retention, _ := time.ParseDuration(s.cfg.Logging.DiagnosticRetention)
	_, _ = s.db.ExecContext(ctx, `UPDATE audit_logs SET full_ip=NULL, user_agent=NULL
		WHERE created_at<=? AND (full_ip IS NOT NULL OR user_agent IS NOT NULL)`, now.Add(-retention).Unix())
	return nil
}

func cleanupFailureText(err error) string {
	if err == nil {
		return "cleanup failed"
	}
	text := strings.Join(strings.Fields(err.Error()), " ")
	runes := []rune(text)
	if len(runes) > 1000 {
		text = string(runes[:1000])
	}
	return text
}

func (s *Service) markCleanupFailure(ctx context.Context, shareID string, cause error) error {
	now := s.now().Unix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE shares SET status='cleanup_pending',cleanup_error=?,
		cleanup_attempts=cleanup_attempts+1,cleanup_next_attempt_at=?,updated_at=?
		WHERE id=? AND status NOT IN ('metadata_only','deleted')`, cleanupFailureText(cause), now+60, now, shareID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE upload_tasks SET status='cancel_requested',
		cancel_requested_at=COALESCE(cancel_requested_at,?),completion_session_id=NULL,updated_at=?
		WHERE share_id=? AND status IN ('uploading','paused')`, now, now, shareID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) expireUploadSessions(ctx context.Context, now time.Time) error {
	rows, err := s.db.QueryContext(ctx, `SELECT u.id, u.share_id, u.backend, u.storage_profile_id, u.s3_upload_id, e.storage_key
		FROM upload_sessions u JOIN entries e ON e.id=u.entry_id
		JOIN upload_tasks t ON t.share_id=u.share_id
		WHERE u.status='open' AND u.expires_at<=? AND t.status!='paused'`, now.Unix())
	if err != nil {
		return err
	}
	type expired struct {
		id, shareID, backend, profileID, key string
		uploadID                             sql.NullString
	}
	var sessions []expired
	for rows.Next() {
		var item expired
		if err := rows.Scan(&item.id, &item.shareID, &item.backend, &item.profileID, &item.uploadID, &item.key); err != nil {
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
	for _, item := range sessions {
		var cleanupErrors []error
		if item.backend == "s3" && item.uploadID.Valid {
			store, openErr := s.storeFor(item.profileID, item.backend)
			if openErr != nil {
				cleanupErrors = append(cleanupErrors, openErr)
			} else if multipart, ok := store.(storage.MultipartStore); !ok {
				cleanupErrors = append(cleanupErrors, errors.New("storage does not support multipart abort"))
			} else if abortErr := multipart.AbortMultipart(ctx, item.key, item.uploadID.String); abortErr != nil {
				cleanupErrors = append(cleanupErrors, abortErr)
			}
		}
		if removeErr := os.RemoveAll(filepath.Join(s.stagingRoot, item.id)); removeErr != nil {
			cleanupErrors = append(cleanupErrors, removeErr)
		}
		if len(cleanupErrors) > 0 {
			_ = s.markCleanupFailure(ctx, item.shareID, errors.Join(cleanupErrors...))
			continue
		}
		if _, err := s.db.ExecContext(ctx, "UPDATE upload_sessions SET status='expired', updated_at=? WHERE id=? AND status='open'",
			now.Unix(), item.id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) cleanupShareContent(ctx context.Context, now time.Time) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id, storage_backend, storage_profile_id FROM shares sh
		WHERE status IN ('expired','revoked','cleanup_pending')
		  AND COALESCE(cleanup_next_attempt_at,0)<=?
		  AND NOT EXISTS (SELECT 1 FROM upload_tasks t WHERE t.share_id=sh.id AND t.status='completing')
		  AND NOT EXISTS (
			SELECT 1 FROM download_actions d WHERE d.share_id=sh.id AND d.expires_at>?
		  )`, now.Unix(), now.Unix())
	if err != nil {
		return err
	}
	type candidate struct{ id, backend, profileID string }
	var candidates []candidate
	for rows.Next() {
		var item candidate
		if err := rows.Scan(&item.id, &item.backend, &item.profileID); err != nil {
			return err
		}
		candidates = append(candidates, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	retention, _ := time.ParseDuration(s.cfg.Sharing.MetadataRetention)
	for _, item := range candidates {
		if err := s.abortUploadSessions(ctx, item.id); err != nil {
			_ = s.markCleanupFailure(ctx, item.id, err)
			continue
		}
		store, err := s.storeFor(item.profileID, item.backend)
		if err != nil {
			_ = s.markCleanupFailure(ctx, item.id, err)
			continue
		}
		if err := store.DeletePrefix(ctx, "shares/"+item.id+"/objects"); err != nil {
			_ = s.markCleanupFailure(ctx, item.id, err)
			continue
		}
		if err := store.DeletePrefix(ctx, "shares/"+item.id+"/zip"); err != nil {
			_ = s.markCleanupFailure(ctx, item.id, err)
			continue
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			_ = s.markCleanupFailure(ctx, item.id, err)
			continue
		}
		if _, err := tx.ExecContext(ctx, "UPDATE entries SET status='deleted' WHERE share_id=? AND kind='file'", item.id); err != nil {
			tx.Rollback()
			_ = s.markCleanupFailure(ctx, item.id, err)
			continue
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM zip_objects WHERE share_id=?", item.id); err != nil {
			tx.Rollback()
			_ = s.markCleanupFailure(ctx, item.id, err)
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE shares SET status='metadata_only',
			content_deleted_at=?, metadata_delete_after=?, cleanup_error=NULL,cleanup_attempts=0,
			cleanup_next_attempt_at=NULL,updated_at=? WHERE id=?`,
			now.Unix(), now.Add(retention).Unix(), now.Unix(), item.id); err != nil {
			tx.Rollback()
			_ = s.markCleanupFailure(ctx, item.id, err)
			continue
		}
		if _, err := tx.ExecContext(ctx, "UPDATE upload_tasks SET status='aborted',updated_at=? WHERE share_id=? AND status='cancel_requested'", now.Unix(), item.id); err != nil {
			tx.Rollback()
			_ = s.markCleanupFailure(ctx, item.id, err)
			continue
		}
		if err := tx.Commit(); err != nil {
			_ = s.markCleanupFailure(ctx, item.id, err)
		}
	}
	return nil
}

func (s *Service) RunBackupIfDue(ctx context.Context) error {
	now := s.now().Local()
	if now.Hour() < s.cfg.Backup.Hour {
		return nil
	}
	date := now.Format("2006-01-02")
	var last string
	_ = s.db.QueryRowContext(ctx, "SELECT value FROM settings WHERE key='last_backup_date'").Scan(&last)
	if last == date {
		return nil
	}
	filename := "sharepipe-" + now.Format("20060102-150405") + ".db"
	destination := filepath.Join(s.cfg.Backup.Directory, filename)
	if err := s.db.Backup(ctx, destination); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO settings(key,value,updated_at) VALUES('last_backup_date',?,?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
		date, now.Unix())
	if err != nil {
		return err
	}
	return pruneBackups(s.cfg.Backup.Directory, s.cfg.Backup.RetentionCount)
}

func pruneBackups(directory string, retain int) error {
	files, err := filepath.Glob(filepath.Join(directory, "sharepipe-*.db"))
	if err != nil {
		return err
	}
	sort.Strings(files)
	for len(files) > retain {
		target := files[0]
		files = files[1:]
		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(directory)+string(filepath.Separator)) {
			return errors.New("backup path escaped its directory")
		}
		if err := os.Remove(target); err != nil {
			return err
		}
	}
	return nil
}
