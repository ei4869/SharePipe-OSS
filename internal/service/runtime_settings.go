package service

import (
	"context"
	"fmt"
	"strconv"

	"github.com/ei4869/SharePipe-OSS/internal/core"
)

type RuntimeSettings struct {
	MaxFileBytes           int64   `json:"maxFileBytes"`
	MaxShareBytes          int64   `json:"maxShareBytes"`
	MaxFilesPerShare       int     `json:"maxFilesPerShare"`
	DefaultExpiry          string  `json:"defaultExpiry"`
	DefaultDownloadLimit   int     `json:"defaultDownloadLimit"`
	ZipAsyncThresholdBytes int64   `json:"zipAsyncThresholdBytes"`
	PairCodeTTLSeconds     int     `json:"pairCodeTTLSeconds"`
	R2BudgetStorageGBMonth float64 `json:"r2BudgetStorageGBMonth"`
	R2BudgetClassA         int64   `json:"r2BudgetClassA"`
	R2BudgetClassB         int64   `json:"r2BudgetClassB"`
}

type UpdateRuntimeSettings struct {
	MaxFileBytes           *int64   `json:"maxFileBytes"`
	MaxShareBytes          *int64   `json:"maxShareBytes"`
	MaxFilesPerShare       *int     `json:"maxFilesPerShare"`
	DefaultExpiry          *string  `json:"defaultExpiry"`
	DefaultDownloadLimit   *int     `json:"defaultDownloadLimit"`
	ZipAsyncThresholdBytes *int64   `json:"zipAsyncThresholdBytes"`
	PairCodeTTLSeconds     *int     `json:"pairCodeTTLSeconds"`
	R2BudgetStorageGBMonth *float64 `json:"r2BudgetStorageGBMonth"`
	R2BudgetClassA         *int64   `json:"r2BudgetClassA"`
	R2BudgetClassB         *int64   `json:"r2BudgetClassB"`
}

func (s *Service) initializeRuntimeSettings(ctx context.Context) error {
	defaults := RuntimeSettings{
		MaxFileBytes: s.cfg.Uploads.MaxFileBytes, MaxShareBytes: s.cfg.Uploads.MaxShareBytes,
		MaxFilesPerShare: s.cfg.Uploads.MaxFilesPerShare, DefaultExpiry: s.cfg.Sharing.DefaultExpiry,
		DefaultDownloadLimit:   s.cfg.Sharing.DefaultDownloadLimit,
		ZipAsyncThresholdBytes: s.cfg.Zip.AsyncThresholdBytes,
		PairCodeTTLSeconds:     600,
		R2BudgetStorageGBMonth: 10,
		R2BudgetClassA:         1_000_000,
		R2BudgetClassB:         10_000_000,
	}
	values := map[string]string{
		"max_file_bytes":             strconv.FormatInt(defaults.MaxFileBytes, 10),
		"max_share_bytes":            strconv.FormatInt(defaults.MaxShareBytes, 10),
		"max_files_per_share":        strconv.Itoa(defaults.MaxFilesPerShare),
		"default_expiry":             defaults.DefaultExpiry,
		"default_download_limit":     strconv.Itoa(defaults.DefaultDownloadLimit),
		"zip_async_threshold_bytes":  strconv.FormatInt(defaults.ZipAsyncThresholdBytes, 10),
		"pair_code_ttl_seconds":      strconv.Itoa(defaults.PairCodeTTLSeconds),
		"r2_budget_storage_gb_month": strconv.FormatFloat(defaults.R2BudgetStorageGBMonth, 'f', -1, 64),
		"r2_budget_class_a":          strconv.FormatInt(defaults.R2BudgetClassA, 10),
		"r2_budget_class_b":          strconv.FormatInt(defaults.R2BudgetClassB, 10),
	}
	now := s.now().Unix()
	for key, value := range values {
		if _, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO settings(key,value,updated_at) VALUES(?,?,?)`,
			key, value, now); err != nil {
			return err
		}
	}
	loaded, err := s.loadRuntimeSettings(ctx)
	if err != nil {
		return err
	}
	s.runtime.Store(loaded)
	return nil
}

func (s *Service) loadRuntimeSettings(ctx context.Context) (RuntimeSettings, error) {
	values := make(map[string]string)
	rows, err := s.db.QueryContext(ctx, `SELECT key,value FROM settings WHERE key IN (
		'max_file_bytes','max_share_bytes','max_files_per_share','default_expiry',
		'default_download_limit','zip_async_threshold_bytes','pair_code_ttl_seconds',
		'r2_budget_storage_gb_month','r2_budget_class_a','r2_budget_class_b'
	)`)
	if err != nil {
		return RuntimeSettings{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return RuntimeSettings{}, err
		}
		values[key] = value
	}
	var settings RuntimeSettings
	if settings.MaxFileBytes, err = strconv.ParseInt(values["max_file_bytes"], 10, 64); err != nil {
		return RuntimeSettings{}, err
	}
	if settings.MaxShareBytes, err = strconv.ParseInt(values["max_share_bytes"], 10, 64); err != nil {
		return RuntimeSettings{}, err
	}
	if settings.MaxFilesPerShare, err = strconv.Atoi(values["max_files_per_share"]); err != nil {
		return RuntimeSettings{}, err
	}
	settings.DefaultExpiry = values["default_expiry"]
	if settings.DefaultDownloadLimit, err = strconv.Atoi(values["default_download_limit"]); err != nil {
		return RuntimeSettings{}, err
	}
	if settings.ZipAsyncThresholdBytes, err = strconv.ParseInt(values["zip_async_threshold_bytes"], 10, 64); err != nil {
		return RuntimeSettings{}, err
	}
	if settings.PairCodeTTLSeconds, err = strconv.Atoi(values["pair_code_ttl_seconds"]); err != nil {
		return RuntimeSettings{}, err
	}
	if settings.R2BudgetStorageGBMonth, err = strconv.ParseFloat(values["r2_budget_storage_gb_month"], 64); err != nil {
		return RuntimeSettings{}, err
	}
	if settings.R2BudgetClassA, err = strconv.ParseInt(values["r2_budget_class_a"], 10, 64); err != nil {
		return RuntimeSettings{}, err
	}
	if settings.R2BudgetClassB, err = strconv.ParseInt(values["r2_budget_class_b"], 10, 64); err != nil {
		return RuntimeSettings{}, err
	}
	if err := s.validateRuntimeSettings(settings); err != nil {
		return RuntimeSettings{}, err
	}
	return settings, rows.Err()
}

func (s *Service) RuntimeSettings() RuntimeSettings {
	return s.runtime.Load().(RuntimeSettings)
}

func (s *Service) UpdateSettings(ctx context.Context, input UpdateRuntimeSettings) (RuntimeSettings, error) {
	s.runtimeMutex.Lock()
	defer s.runtimeMutex.Unlock()
	current := s.RuntimeSettings()
	if input.MaxFileBytes != nil {
		current.MaxFileBytes = *input.MaxFileBytes
	}
	if input.MaxShareBytes != nil {
		current.MaxShareBytes = *input.MaxShareBytes
	}
	if input.MaxFilesPerShare != nil {
		current.MaxFilesPerShare = *input.MaxFilesPerShare
	}
	if input.DefaultExpiry != nil {
		current.DefaultExpiry = *input.DefaultExpiry
	}
	if input.DefaultDownloadLimit != nil {
		current.DefaultDownloadLimit = *input.DefaultDownloadLimit
	}
	if input.ZipAsyncThresholdBytes != nil {
		current.ZipAsyncThresholdBytes = *input.ZipAsyncThresholdBytes
	}
	if input.PairCodeTTLSeconds != nil {
		current.PairCodeTTLSeconds = *input.PairCodeTTLSeconds
	}
	if input.R2BudgetStorageGBMonth != nil {
		current.R2BudgetStorageGBMonth = *input.R2BudgetStorageGBMonth
	}
	if input.R2BudgetClassA != nil {
		current.R2BudgetClassA = *input.R2BudgetClassA
	}
	if input.R2BudgetClassB != nil {
		current.R2BudgetClassB = *input.R2BudgetClassB
	}
	if err := s.validateRuntimeSettings(current); err != nil {
		return RuntimeSettings{}, err
	}
	values := map[string]string{
		"max_file_bytes":             strconv.FormatInt(current.MaxFileBytes, 10),
		"max_share_bytes":            strconv.FormatInt(current.MaxShareBytes, 10),
		"max_files_per_share":        strconv.Itoa(current.MaxFilesPerShare),
		"default_expiry":             current.DefaultExpiry,
		"default_download_limit":     strconv.Itoa(current.DefaultDownloadLimit),
		"zip_async_threshold_bytes":  strconv.FormatInt(current.ZipAsyncThresholdBytes, 10),
		"pair_code_ttl_seconds":      strconv.Itoa(current.PairCodeTTLSeconds),
		"r2_budget_storage_gb_month": strconv.FormatFloat(current.R2BudgetStorageGBMonth, 'f', -1, 64),
		"r2_budget_class_a":          strconv.FormatInt(current.R2BudgetClassA, 10),
		"r2_budget_class_b":          strconv.FormatInt(current.R2BudgetClassB, 10),
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return RuntimeSettings{}, err
	}
	defer tx.Rollback()
	now := s.now().Unix()
	for key, value := range values {
		if _, err := tx.ExecContext(ctx, "UPDATE settings SET value=?, updated_at=? WHERE key=?", value, now, key); err != nil {
			return RuntimeSettings{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return RuntimeSettings{}, err
	}
	s.runtime.Store(current)
	return current, nil
}

func (s *Service) validateRuntimeSettings(settings RuntimeSettings) error {
	if settings.MaxFileBytes < s.cfg.Uploads.ChunkSizeBytes {
		return fmt.Errorf("%w: maxFileBytes must be at least one upload chunk", core.ErrValidation)
	}
	if settings.MaxShareBytes < settings.MaxFileBytes {
		return fmt.Errorf("%w: maxShareBytes must be at least maxFileBytes", core.ErrValidation)
	}
	if settings.MaxFilesPerShare < 1 || settings.MaxFilesPerShare > 100000 {
		return fmt.Errorf("%w: maxFilesPerShare is outside the supported range", core.ErrValidation)
	}
	if _, ok := s.cfg.ExpiryAllowed(settings.DefaultExpiry); !ok {
		return fmt.Errorf("%w: defaultExpiry is not an allowed preset", core.ErrValidation)
	}
	if settings.DefaultDownloadLimit < 0 {
		return fmt.Errorf("%w: defaultDownloadLimit cannot be negative", core.ErrValidation)
	}
	if settings.ZipAsyncThresholdBytes < 64*1024*1024 {
		return fmt.Errorf("%w: ZIP threshold must be at least 64 MiB", core.ErrValidation)
	}
	if settings.PairCodeTTLSeconds < 300 || settings.PairCodeTTLSeconds > 86400 {
		return fmt.Errorf("%w: pairCodeTTLSeconds must be between 300 and 86400", core.ErrValidation)
	}
	if settings.R2BudgetStorageGBMonth <= 0 || settings.R2BudgetStorageGBMonth > 1_000_000 {
		return fmt.Errorf("%w: r2BudgetStorageGBMonth is outside the supported range", core.ErrValidation)
	}
	if settings.R2BudgetClassA < 1 || settings.R2BudgetClassA > 1_000_000_000_000 {
		return fmt.Errorf("%w: r2BudgetClassA is outside the supported range", core.ErrValidation)
	}
	if settings.R2BudgetClassB < 1 || settings.R2BudgetClassB > 1_000_000_000_000 {
		return fmt.Errorf("%w: r2BudgetClassB is outside the supported range", core.ErrValidation)
	}
	return nil
}
