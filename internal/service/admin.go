package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"

	"github.com/ei4869/SharePipe-OSS/internal/security"
	"github.com/ei4869/SharePipe-OSS/internal/storage"
)

type StorageHealth struct {
	ID       string `json:"id,omitempty"`
	Name     string `json:"name"`
	Kind     string `json:"kind,omitempty"`
	Provider string `json:"provider,omitempty"`
	Healthy  bool   `json:"healthy"`
	Error    string `json:"error,omitempty"`
	Default  bool   `json:"default"`
}

type Overview struct {
	ActiveShares   int64           `json:"activeShares"`
	StorageBytes   int64           `json:"storageBytes"`
	DownloadsToday int64           `json:"downloadsToday"`
	PendingCleanup int64           `json:"pendingCleanup"`
	Storage        []StorageHealth `json:"storage"`
}

func (s *Service) AdminOverview(ctx context.Context) (Overview, error) {
	var overview Overview
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM shares WHERE status='active'").Scan(&overview.ActiveShares); err != nil {
		return Overview{}, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(total_bytes),0) FROM shares
		WHERE status IN ('uploading','active','expired','revoked','cleanup_pending')`).Scan(&overview.StorageBytes); err != nil {
		return Overview{}, err
	}
	midnight := s.now().Local()
	midnight = time.Date(midnight.Year(), midnight.Month(), midnight.Day(), 0, 0, 0, 0, midnight.Location())
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM download_actions WHERE created_at>=?", midnight.Unix()).Scan(&overview.DownloadsToday); err != nil {
		return Overview{}, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM shares
		WHERE status IN ('expired','revoked','cleanup_pending')`).Scan(&overview.PendingCleanup); err != nil {
		return Overview{}, err
	}
	defaultProfile, _ := s.DefaultStorage(ctx)
	profiles, err := s.ListStorageProfiles(ctx)
	if err != nil {
		return Overview{}, err
	}
	for _, profile := range profiles {
		if !profile.Enabled {
			continue
		}
		health := StorageHealth{
			ID: profile.ID, Name: profile.Name, Kind: profile.Kind, Provider: profile.Provider,
			Default: profile.ID == defaultProfile, Healthy: profile.LastCheckedAt != nil && profile.LastError == "",
			Error: profile.LastError,
		}
		if profile.LastCheckedAt == nil {
			health.Error = "尚未执行连接检测"
		}
		overview.Storage = append(overview.Storage, health)
	}
	return overview, nil
}

func (s *Service) CheckStorage(ctx context.Context, name string) StorageHealth {
	result := StorageHealth{Name: name}
	store, err := s.Store(name)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	return checkStore(ctx, name, store)
}

func checkStore(ctx context.Context, name string, store storage.Store) StorageHealth {
	result := StorageHealth{Name: name}
	id, _, err := security.RandomToken(16)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	key := "health/" + id
	content := []byte("sharepipe storage health")
	if _, err := store.Put(ctx, key, bytes.NewReader(content), int64(len(content))); err != nil {
		result.Error = err.Error()
		return result
	}
	defer store.Delete(ctx, key)
	object, err := store.Open(ctx, key, nil)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	defer object.Body.Close()
	actual, err := io.ReadAll(object.Body)
	if err != nil || !bytes.Equal(actual, content) {
		result.Error = fmt.Sprintf("storage read verification failed: %v", err)
		return result
	}
	result.Healthy = true
	return result
}

func (s *Service) EnableDiagnosticMode(ctx context.Context) (time.Time, error) {
	until := s.now().Add(24 * time.Hour).UTC()
	_, err := s.db.ExecContext(ctx, `INSERT INTO settings(key,value,updated_at) VALUES('diagnostic_until',?,?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
		fmt.Sprintf("%d", until.Unix()), s.now().Unix())
	return until, err
}
