package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/ei4869/SharePipe-OSS/internal/core"
	"github.com/ei4869/SharePipe-OSS/internal/security"
	"github.com/ei4869/SharePipe-OSS/internal/storage"
)

type DownloadResource struct {
	ActionID         string
	ShareID          string
	Kind             string
	Filename         string
	MIMEType         string
	StorageBackend   string
	StorageProfileID string
	StorageKey       string
	Entry            *core.Entry
	ExpiresAt        time.Time
	StreamingZip     bool
}

func (s *Service) CreateDownloadGrant(ctx context.Context, publicToken string, unlocked bool, entryID, kind string) (core.DownloadGrant, error) {
	public, err := s.GetPublicShare(ctx, publicToken, unlocked)
	if err != nil {
		return core.DownloadGrant{}, err
	}
	if public.Locked {
		return core.DownloadGrant{}, core.ErrForbidden
	}
	var entry *core.Entry
	status := "ready"
	if kind == "file" {
		for index := range public.Entries {
			if public.Entries[index].ID == entryID && public.Entries[index].Kind == "file" && public.Entries[index].Status == "ready" {
				entry = &public.Entries[index]
				break
			}
		}
		if entry == nil {
			return core.DownloadGrant{}, core.ErrNotFound
		}
	} else if kind == "zip" {
		if public.Share.TotalBytes > s.RuntimeSettings().ZipAsyncThresholdBytes {
			ready, err := s.ensureZipJob(ctx, public.Share)
			if err != nil {
				return core.DownloadGrant{}, err
			}
			if !ready {
				status = "preparing"
			}
		}
	} else {
		return core.DownloadGrant{}, fmt.Errorf("%w: unsupported download kind", core.ErrValidation)
	}

	actionID, _, err := security.RandomToken(16)
	if err != nil {
		return core.DownloadGrant{}, err
	}
	token, digest, err := security.RandomToken(32)
	if err != nil {
		return core.DownloadGrant{}, err
	}
	expiresAt := s.now().Add(time.Duration(s.cfg.Sharing.GrantTTLMinutes) * time.Minute).UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return core.DownloadGrant{}, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE shares SET download_count=download_count+1, updated_at=?
		WHERE id=? AND status='active' AND expires_at>?
		  AND (download_limit IS NULL OR download_count < download_limit)`,
		s.now().Unix(), public.Share.ID, s.now().Unix())
	if err != nil {
		return core.DownloadGrant{}, err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return core.DownloadGrant{}, core.ErrDownloadLimit
	}
	var entryValue any
	if entry != nil {
		entryValue = entry.ID
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO download_actions(
		id, share_id, entry_id, kind, token_hash, status, expires_at, created_at
	) VALUES(?, ?, ?, ?, ?, ?, ?, ?)`,
		actionID, public.Share.ID, entryValue, kind, digest, status, expiresAt.Unix(), s.now().Unix()); err != nil {
		return core.DownloadGrant{}, err
	}
	if err := tx.Commit(); err != nil {
		return core.DownloadGrant{}, err
	}
	return core.DownloadGrant{
		Token: token, URL: strings.TrimRight(s.cfg.Server.PublicBaseURL, "/") + "/api/v1/downloads/" + token,
		Status: status, ExpiresAt: expiresAt,
	}, nil
}

func (s *Service) GetDownloadGrant(ctx context.Context, token string) (core.DownloadGrant, error) {
	var status, kind, shareID string
	var expires int64
	err := s.db.QueryRowContext(ctx, `SELECT status, kind, share_id, expires_at
		FROM download_actions WHERE token_hash=?`, security.HashToken(token)).Scan(&status, &kind, &shareID, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return core.DownloadGrant{}, core.ErrNotFound
	}
	if err != nil {
		return core.DownloadGrant{}, err
	}
	expiresAt := time.Unix(expires, 0).UTC()
	if !expiresAt.After(s.now()) {
		return core.DownloadGrant{}, core.ErrExpired
	}
	if kind == "zip" && status == "preparing" {
		var zipStatus string
		if err := s.db.QueryRowContext(ctx, "SELECT status FROM zip_objects WHERE share_id=?", shareID).Scan(&zipStatus); err == nil {
			if zipStatus == "ready" {
				status = "ready"
				_, _ = s.db.ExecContext(ctx, "UPDATE download_actions SET status='ready' WHERE token_hash=?", security.HashToken(token))
			} else if zipStatus == "failed" {
				status = "failed"
				_, _ = s.db.ExecContext(ctx, "UPDATE download_actions SET status='failed' WHERE token_hash=?", security.HashToken(token))
			}
		}
	}
	return core.DownloadGrant{
		Token: token, URL: strings.TrimRight(s.cfg.Server.PublicBaseURL, "/") + "/api/v1/downloads/" + token,
		Status: status, ExpiresAt: expiresAt,
	}, nil
}

func (s *Service) ResolveDownload(ctx context.Context, token string) (DownloadResource, error) {
	var resource DownloadResource
	var entryID sql.NullString
	var expires int64
	var actionStatus string
	err := s.db.QueryRowContext(ctx, `SELECT d.id, d.share_id, d.kind, d.entry_id, d.expires_at, d.status,
		sh.name, sh.storage_backend, sh.storage_profile_id
		FROM download_actions d JOIN shares sh ON sh.id=d.share_id
		WHERE d.token_hash=?`, security.HashToken(token)).Scan(
		&resource.ActionID, &resource.ShareID, &resource.Kind, &entryID, &expires, &actionStatus,
		&resource.Filename, &resource.StorageBackend, &resource.StorageProfileID)
	if errors.Is(err, sql.ErrNoRows) {
		return DownloadResource{}, core.ErrNotFound
	}
	if err != nil {
		return DownloadResource{}, err
	}
	resource.ExpiresAt = time.Unix(expires, 0).UTC()
	if !resource.ExpiresAt.After(s.now()) {
		return DownloadResource{}, core.ErrExpired
	}
	if actionStatus == "failed" {
		return DownloadResource{}, core.ErrConflict
	}
	if resource.Kind == "file" {
		var entry core.Entry
		var created int64
		err := s.db.QueryRowContext(ctx, `SELECT id, share_id, path, kind, size, mime_type,
			storage_key, COALESCE(sha256,''), status, created_at FROM entries WHERE id=?`, entryID.String).Scan(
			&entry.ID, &entry.ShareID, &entry.Path, &entry.Kind, &entry.Size, &entry.MIMEType,
			&entry.StorageKey, &entry.SHA256, &entry.Status, &created)
		if err != nil || entry.Status != "ready" {
			return DownloadResource{}, core.ErrNotFound
		}
		entry.CreatedAt = time.Unix(created, 0).UTC()
		resource.Entry = &entry
		resource.Filename = path.Base(entry.Path)
		resource.MIMEType = entry.MIMEType
		resource.StorageKey = entry.StorageKey
		return resource, nil
	}
	if actionStatus == "preparing" {
		grant, err := s.GetDownloadGrant(ctx, token)
		if err != nil || grant.Status != "ready" {
			return DownloadResource{}, core.ErrConflict
		}
	}
	resource.Filename += ".zip"
	resource.MIMEType = "application/zip"
	if s.shareStreamsZip(ctx, resource.ShareID) {
		resource.StreamingZip = true
		return resource, nil
	}
	var zipStatus string
	if err := s.db.QueryRowContext(ctx, `SELECT storage_key, status FROM zip_objects
		WHERE share_id=?`, resource.ShareID).Scan(&resource.StorageKey, &zipStatus); err != nil {
		return DownloadResource{}, core.ErrConflict
	}
	if zipStatus != "ready" {
		return DownloadResource{}, core.ErrConflict
	}
	return resource, nil
}

func (s *Service) ResolvePreview(ctx context.Context, publicToken string, unlocked bool, entryID string) (DownloadResource, error) {
	public, err := s.GetPublicShare(ctx, publicToken, unlocked)
	if err != nil {
		return DownloadResource{}, err
	}
	if public.Locked {
		return DownloadResource{}, core.ErrForbidden
	}
	for index := range public.Entries {
		entry := public.Entries[index]
		if entry.ID != entryID || entry.Kind != "file" || entry.Status != "ready" {
			continue
		}
		if !PreviewAllowed(entry.MIMEType, entry.Path) {
			return DownloadResource{}, core.ErrForbidden
		}
		return DownloadResource{
			ShareID: public.Share.ID, Kind: "preview", Filename: path.Base(entry.Path),
			MIMEType: entry.MIMEType, StorageBackend: public.Share.StorageBackend,
			StorageProfileID: public.Share.StorageProfileID,
			StorageKey:       entry.StorageKey, Entry: &entry,
		}, nil
	}
	return DownloadResource{}, core.ErrNotFound
}

func PreviewAllowed(mimeType, filename string) bool {
	mimeType = strings.ToLower(strings.Split(mimeType, ";")[0])
	if mimeType == "text/html" || mimeType == "application/xhtml+xml" || mimeType == "image/svg+xml" {
		return false
	}
	switch {
	case mimeType == "application/pdf":
		return true
	case strings.HasPrefix(mimeType, "audio/"), strings.HasPrefix(mimeType, "video/"):
		return true
	case mimeType == "image/jpeg", mimeType == "image/png", mimeType == "image/gif", mimeType == "image/webp":
		return true
	case strings.HasPrefix(mimeType, "text/"):
		return true
	case mimeType == "application/json", mimeType == "application/xml":
		return true
	}
	extension := strings.ToLower(path.Ext(filename))
	switch extension {
	case ".txt", ".md", ".go", ".rs", ".py", ".js", ".jsx", ".ts", ".tsx", ".css", ".json", ".xml", ".yaml", ".yml", ".toml", ".sql", ".sh", ".ps1":
		return true
	}
	return false
}

func (s *Service) OpenResource(ctx context.Context, resource DownloadResource, byteRange *storage.ByteRange) (storage.Object, error) {
	store, err := s.storeFor(resource.StorageProfileID, resource.StorageBackend)
	if err != nil {
		return storage.Object{}, err
	}
	return store.Open(ctx, resource.StorageKey, byteRange)
}

func (s *Service) StatResource(ctx context.Context, resource DownloadResource) (storage.ObjectInfo, error) {
	store, err := s.storeFor(resource.StorageProfileID, resource.StorageBackend)
	if err != nil {
		return storage.ObjectInfo{}, err
	}
	return store.Stat(ctx, resource.StorageKey)
}
