package service

import (
	"archive/zip"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ei4869/SharePipe-OSS/internal/core"
	"github.com/ei4869/SharePipe-OSS/internal/security"
	"github.com/ei4869/SharePipe-OSS/internal/storage"
)

func (s *Service) shareStreamsZip(ctx context.Context, shareID string) bool {
	var total int64
	if err := s.db.QueryRowContext(ctx, "SELECT total_bytes FROM shares WHERE id=?", shareID).Scan(&total); err != nil {
		return false
	}
	return total <= s.RuntimeSettings().ZipAsyncThresholdBytes
}

func (s *Service) ensureZipJob(ctx context.Context, share core.Share) (bool, error) {
	var status string
	var expires sql.NullInt64
	err := s.db.QueryRowContext(ctx, "SELECT status, expires_at FROM zip_objects WHERE share_id=?", share.ID).Scan(&status, &expires)
	if err == nil && status == "ready" && expires.Valid && expires.Int64 > s.now().Unix() {
		return true, nil
	}
	if err == nil && status == "preparing" {
		return false, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	jobID, _, err := security.RandomToken(16)
	if err != nil {
		return false, err
	}
	payload, _ := json.Marshal(map[string]string{"shareId": share.ID})
	now := s.now().Unix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO zip_objects(share_id, storage_key, status, updated_at)
		VALUES(?, '', 'preparing', ?)
		ON CONFLICT(share_id) DO UPDATE SET storage_key='', status='preparing', expires_at=NULL, error=NULL, updated_at=excluded.updated_at`,
		share.ID, now); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO jobs(id, kind, payload, state, run_at, created_at, updated_at)
		VALUES(?, 'build_zip', ?, 'queued', ?, ?, ?)`, jobID, string(payload), now, now, now); err != nil {
		return false, err
	}
	return false, tx.Commit()
}

func (s *Service) WriteStreamingZip(ctx context.Context, shareID string, destination io.Writer) error {
	share, entries, err := s.zipSource(ctx, shareID)
	if err != nil {
		return err
	}
	store, err := s.storeFor(share.StorageProfileID, share.StorageBackend)
	if err != nil {
		return err
	}
	archive := zip.NewWriter(destination)
	for _, entry := range entries {
		header := &zip.FileHeader{Name: strings.ReplaceAll(entry.Path, "\\", "/")}
		header.SetModTime(entry.CreatedAt)
		if entry.Kind == "directory" {
			header.Name = strings.TrimRight(header.Name, "/") + "/"
			header.Method = zip.Store
			if _, err := archive.CreateHeader(header); err != nil {
				archive.Close()
				return err
			}
			continue
		}
		if shouldCompress(entry.MIMEType) {
			header.Method = zip.Deflate
		} else {
			header.Method = zip.Store
		}
		writer, err := archive.CreateHeader(header)
		if err != nil {
			archive.Close()
			return err
		}
		object, err := store.Open(ctx, entry.StorageKey, nil)
		if err != nil {
			archive.Close()
			return err
		}
		_, copyErr := io.Copy(writer, object.Body)
		closeErr := object.Body.Close()
		if copyErr != nil {
			archive.Close()
			return copyErr
		}
		if closeErr != nil {
			archive.Close()
			return closeErr
		}
	}
	return archive.Close()
}

func (s *Service) BuildCachedZip(ctx context.Context, shareID string) error {
	share, _, err := s.zipSource(ctx, shareID)
	if err != nil {
		return s.failZip(ctx, shareID, err)
	}
	tempDirectory := filepath.Join(s.stagingRoot, "zip")
	if err := os.MkdirAll(tempDirectory, 0o750); err != nil {
		return s.failZip(ctx, shareID, err)
	}
	file, err := os.CreateTemp(tempDirectory, "share-"+shareID+"-*.zip")
	if err != nil {
		return s.failZip(ctx, shareID, err)
	}
	tempPath := file.Name()
	defer os.Remove(tempPath)
	if err := s.WriteStreamingZip(ctx, shareID, file); err != nil {
		file.Close()
		return s.failZip(ctx, shareID, err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return s.failZip(ctx, shareID, err)
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return s.failZip(ctx, shareID, err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		file.Close()
		return s.failZip(ctx, shareID, err)
	}
	key := storage.ZipKey(shareID, s.now())
	store, err := s.storeFor(share.StorageProfileID, share.StorageBackend)
	if err != nil {
		file.Close()
		return s.failZip(ctx, shareID, err)
	}
	if _, err := store.Put(ctx, key, file, info.Size()); err != nil {
		file.Close()
		return s.failZip(ctx, shareID, err)
	}
	if err := file.Close(); err != nil {
		return s.failZip(ctx, shareID, err)
	}
	cacheTTL, _ := time.ParseDuration(s.cfg.Zip.CacheTTL)
	_, err = s.db.ExecContext(ctx, `UPDATE zip_objects SET storage_key=?, size=?, status='ready',
		expires_at=?, error=NULL, updated_at=? WHERE share_id=?`,
		key, info.Size(), s.now().Add(cacheTTL).Unix(), s.now().Unix(), shareID)
	return err
}

func (s *Service) zipSource(ctx context.Context, shareID string) (core.Share, []core.Entry, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, name, description, storage_backend, storage_profile_id, status, expires_at,
		download_limit, download_count, file_count, total_bytes, password_hash,
		content_deleted_at, metadata_delete_after, created_at, updated_at,pairing_enabled,qr_enabled FROM shares WHERE id=?`, shareID)
	share, _, err := scanShare(row)
	if err != nil {
		return core.Share{}, nil, err
	}
	if share.Status != "active" && share.Status != "revoked" && share.Status != "expired" {
		return core.Share{}, nil, core.ErrConflict
	}
	entries, err := s.listEntries(ctx, shareID)
	if err != nil {
		return core.Share{}, nil, err
	}
	for _, entry := range entries {
		if entry.Kind == "file" && entry.Status != "ready" {
			return core.Share{}, nil, core.ErrUploadIncomplete
		}
	}
	return share, entries, nil
}

func (s *Service) failZip(ctx context.Context, shareID string, problem error) error {
	_, _ = s.db.ExecContext(ctx, `UPDATE zip_objects SET status='failed', error=?, updated_at=? WHERE share_id=?`,
		problem.Error(), s.now().Unix(), shareID)
	return problem
}

func shouldCompress(mimeType string) bool {
	return strings.HasPrefix(mimeType, "text/") ||
		mimeType == "application/json" || mimeType == "application/xml" ||
		mimeType == "image/svg+xml"
}

func (s *Service) cleanupExpiredZip(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT z.share_id, z.storage_key, sh.storage_backend, sh.storage_profile_id
		FROM zip_objects z JOIN shares sh ON sh.id=z.share_id
		WHERE z.status='ready' AND z.expires_at <= ?`, s.now().Unix())
	if err != nil {
		return err
	}
	defer rows.Close()
	type expiredZip struct{ shareID, key, backend, profileID string }
	var expired []expiredZip
	for rows.Next() {
		var item expiredZip
		if err := rows.Scan(&item.shareID, &item.key, &item.backend, &item.profileID); err != nil {
			return err
		}
		expired = append(expired, item)
	}
	for _, item := range expired {
		store, err := s.storeFor(item.profileID, item.backend)
		if err != nil {
			continue
		}
		if err := store.Delete(ctx, item.key); err != nil {
			continue
		}
		_, _ = s.db.ExecContext(ctx, "DELETE FROM zip_objects WHERE share_id=?", item.shareID)
	}
	return rows.Err()
}
