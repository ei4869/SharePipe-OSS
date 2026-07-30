package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ei4869/SharePipe-OSS/internal/core"
	"github.com/ei4869/SharePipe-OSS/internal/security"
	"github.com/ei4869/SharePipe-OSS/internal/storage"
)

type StorageProfileConfig struct {
	RootPath             string `json:"rootPath,omitempty"`
	AccountID            string `json:"accountId,omitempty"`
	Jurisdiction         string `json:"jurisdiction,omitempty"`
	Endpoint             string `json:"endpoint,omitempty"`
	Region               string `json:"region,omitempty"`
	Bucket               string `json:"bucket,omitempty"`
	Prefix               string `json:"prefix,omitempty"`
	PathStyle            bool   `json:"pathStyle,omitempty"`
	ServerSideEncryption string `json:"serverSideEncryption,omitempty"`
}

type StorageCredentials struct {
	AccessKeyID       string `json:"accessKeyId,omitempty"`
	SecretAccessKey   string `json:"secretAccessKey,omitempty"`
	SessionToken      string `json:"sessionToken,omitempty"`
	ClearSessionToken bool   `json:"clearSessionToken,omitempty"`
}

type StorageProfileInput struct {
	Name        string               `json:"name"`
	Provider    string               `json:"provider"`
	Config      StorageProfileConfig `json:"config"`
	Credentials StorageCredentials   `json:"credentials"`
}

type StorageProfile struct {
	ID                     string               `json:"id"`
	Name                   string               `json:"name"`
	Kind                   string               `json:"kind"`
	Provider               string               `json:"provider"`
	Config                 StorageProfileConfig `json:"config"`
	Enabled                bool                 `json:"enabled"`
	HasCredentials         bool                 `json:"hasCredentials"`
	UsesDefaultCredentials bool                 `json:"usesDefaultCredentials"`
	HasSessionToken        bool                 `json:"hasSessionToken"`
	Default                bool                 `json:"default"`
	InUse                  int64                `json:"inUse"`
	LastCheckedAt          *time.Time           `json:"lastCheckedAt,omitempty"`
	LastError              string               `json:"lastError,omitempty"`
	CreatedAt              time.Time            `json:"createdAt"`
	UpdatedAt              time.Time            `json:"updatedAt"`
}

type StorageProviderDefinition struct {
	ID               string `json:"id"`
	Label            string `json:"label"`
	Kind             string `json:"kind"`
	DocumentationURL string `json:"documentationUrl"`
	Validation       string `json:"validation"`
}

type storageProfileRecord struct {
	StorageProfile
	credentialsEncrypted string
}

var accountIDPattern = regexp.MustCompile(`^[a-fA-F0-9]{32}$`)

func StorageProviderCatalog() []StorageProviderDefinition {
	return []StorageProviderDefinition{
		{ID: "local", Label: "本地磁盘", Kind: "local", DocumentationURL: "https://pkg.go.dev/path/filepath", Validation: "built-in"},
		{ID: "nas", Label: "NAS / 已挂载网络共享", Kind: "local", DocumentationURL: "https://learn.microsoft.com/windows-server/storage/file-server/file-server-smb-overview", Validation: "mounted-filesystem"},
		{ID: "cloudflare-r2", Label: "Cloudflare R2", Kind: "s3", DocumentationURL: "https://developers.cloudflare.com/r2/api/s3/api/", Validation: "live-tested"},
		{ID: "aws-s3", Label: "Amazon S3", Kind: "s3", DocumentationURL: "https://docs.aws.amazon.com/AmazonS3/latest/userguide/Welcome.html", Validation: "official-spec"},
		{ID: "backblaze-b2", Label: "Backblaze B2", Kind: "s3", DocumentationURL: "https://www.backblaze.com/docs/en/cloud-storage-call-the-s3-compatible-api", Validation: "official-spec"},
		{ID: "wasabi", Label: "Wasabi", Kind: "s3", DocumentationURL: "https://docs.wasabi.com/docs/service-urls-for-wasabis-storage-regions", Validation: "official-spec"},
		{ID: "digitalocean-spaces", Label: "DigitalOcean Spaces", Kind: "s3", DocumentationURL: "https://docs.digitalocean.com/products/spaces/reference/s3-compatibility/", Validation: "official-spec"},
		{ID: "minio", Label: "MinIO", Kind: "s3", DocumentationURL: "https://min.io/docs/minio/linux/administration/identity-access-management.html", Validation: "official-spec"},
		{ID: "generic-s3", Label: "通用 S3 兼容服务", Kind: "s3", DocumentationURL: "https://docs.aws.amazon.com/sdk-for-go/v2/developer-guide/configure-endpoints.html", Validation: "contract-only"},
	}
}

func (s *Service) initializeStorageProfiles(ctx context.Context) error {
	now := s.now().Unix()
	var profileCount int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM storage_profiles").Scan(&profileCount); err != nil {
		return err
	}
	if profileCount == 0 {
		localConfig := StorageProfileConfig{RootPath: s.cfg.Storage.Local.Root}
		if err := s.insertBootstrapStorageProfile(ctx, "local", "本地磁盘", "local", "local", localConfig, StorageCredentials{}, now); err != nil {
			return err
		}
		if s.cfg.Storage.S3.Enabled {
			provider := detectLegacyS3Provider(s.cfg.Storage.S3.Endpoint)
			profileConfig := StorageProfileConfig{
				Endpoint: s.cfg.Storage.S3.Endpoint, Region: s.cfg.Storage.S3.Region,
				Bucket: s.cfg.Storage.S3.Bucket, Prefix: s.cfg.Storage.S3.Prefix,
				PathStyle:            s.cfg.Storage.S3.PathStyle,
				ServerSideEncryption: s.cfg.Storage.S3.ServerSideEncryption,
			}
			if provider == "cloudflare-r2" {
				profileConfig.AccountID, profileConfig.Jurisdiction = parseR2Endpoint(profileConfig.Endpoint)
			}
			credentials := StorageCredentials{
				AccessKeyID: s.cfg.S3AccessKeyID, SecretAccessKey: s.cfg.S3SecretAccessKey,
				SessionToken: s.cfg.S3SessionToken,
			}
			name, kind, profileConfig, credentials, err := normalizeStorageProfileInput(StorageProfileInput{
				Name: storageProviderLabel(provider), Provider: provider, Config: profileConfig, Credentials: credentials,
			}, StorageCredentials{}, false)
			if err != nil {
				return fmt.Errorf("normalize bootstrap S3 profile: %w", err)
			}
			if err := s.insertBootstrapStorageProfile(ctx, "s3", name, kind, provider, profileConfig, credentials, now); err != nil {
				return err
			}
		}
	}
	if _, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO settings(key,value,updated_at)
		VALUES('default_storage_profile',?,?)`, s.cfg.Storage.DefaultBackend, now); err != nil {
		return err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,kind,provider,config_json,
		credentials_encrypted,enabled,last_checked_at,COALESCE(last_error,''),created_at,updated_at
		FROM storage_profiles WHERE enabled=1 ORDER BY created_at,id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	loaded := make(map[string]storage.Store)
	for rows.Next() {
		record, err := scanStorageProfile(rows)
		if err != nil {
			return err
		}
		credentials, err := s.decryptStorageCredentials(record.ID, record.credentialsEncrypted)
		if err != nil {
			return fmt.Errorf("decrypt storage profile %s: %w", record.ID, err)
		}
		store, err := buildStorageStore(ctx, record.Provider, record.Kind, record.Config, credentials)
		if err != nil {
			return fmt.Errorf("load storage profile %s: %w", record.ID, err)
		}
		loaded[record.ID] = store
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if loaded["local"] == nil {
		return errors.New("local storage profile is unavailable")
	}
	s.storesMutex.Lock()
	s.stores = loaded
	s.storesMutex.Unlock()
	defaultProfile, err := s.DefaultStorage(ctx)
	if err != nil {
		return err
	}
	_ = defaultProfile
	return nil
}

func (s *Service) insertBootstrapStorageProfile(ctx context.Context, id, name, kind, provider string, cfg StorageProfileConfig, credentials StorageCredentials, now int64) error {
	configJSON, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	encrypted := ""
	if kind == "s3" {
		encrypted, err = s.encryptStorageCredentials(id, credentials)
		if err != nil {
			return err
		}
	}
	_, err = s.db.ExecContext(ctx, `INSERT OR IGNORE INTO storage_profiles(
		id,name,kind,provider,config_json,credentials_encrypted,enabled,created_at,updated_at
	) VALUES(?,?,?,?,?,?,1,?,?)`, id, name, kind, provider, string(configJSON), encrypted, now, now)
	return err
}

func (s *Service) ListStorageProfiles(ctx context.Context) ([]StorageProfile, error) {
	defaultID, _ := s.DefaultStorage(ctx)
	rows, err := s.db.QueryContext(ctx, `SELECT p.id,p.name,p.kind,p.provider,p.config_json,
		p.credentials_encrypted,p.enabled,p.last_checked_at,COALESCE(p.last_error,''),p.created_at,p.updated_at,
		(SELECT COUNT(*) FROM shares sh WHERE sh.storage_profile_id=p.id AND sh.status!='deleted')
		FROM storage_profiles p ORDER BY p.name,p.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]StorageProfile, 0)
	for rows.Next() {
		record, err := scanStorageProfileWithUsage(rows)
		if err != nil {
			return nil, err
		}
		record.Default = record.ID == defaultID
		if err := s.setStorageCredentialMetadata(&record); err != nil {
			return nil, err
		}
		result = append(result, record.StorageProfile)
	}
	return result, rows.Err()
}

func (s *Service) CreateStorageProfile(ctx context.Context, input StorageProfileInput) (StorageProfile, error) {
	idToken, _, err := security.RandomToken(16)
	if err != nil {
		return StorageProfile{}, err
	}
	id := "profile_" + idToken
	name, kind, cfg, credentials, err := normalizeStorageProfileInput(input, StorageCredentials{}, false)
	if err != nil {
		return StorageProfile{}, err
	}
	store, err := buildStorageStore(ctx, input.Provider, kind, cfg, credentials)
	if err != nil {
		return StorageProfile{}, fmt.Errorf("%w: %v", core.ErrValidation, err)
	}
	configJSON, _ := json.Marshal(cfg)
	encrypted := ""
	if kind == "s3" {
		encrypted, err = s.encryptStorageCredentials(id, credentials)
		if err != nil {
			return StorageProfile{}, err
		}
	}
	now := s.now().Unix()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO storage_profiles(
		id,name,kind,provider,config_json,credentials_encrypted,enabled,created_at,updated_at
	) VALUES(?,?,?,?,?,?,1,?,?)`, id, name, kind, input.Provider, string(configJSON), encrypted, now, now); err != nil {
		return StorageProfile{}, err
	}
	s.storesMutex.Lock()
	s.stores[id] = store
	s.storesMutex.Unlock()
	return s.storageProfile(ctx, id)
}

func (s *Service) UpdateStorageProfile(ctx context.Context, id string, input StorageProfileInput) (StorageProfile, error) {
	existing, err := s.storageProfileRecord(ctx, id)
	if err != nil {
		return StorageProfile{}, err
	}
	if id == "local" && input.Provider != "local" {
		return StorageProfile{}, fmt.Errorf("%w: the built-in local profile cannot change provider", core.ErrConflict)
	}
	currentCredentials, err := s.decryptStorageCredentials(id, existing.credentialsEncrypted)
	if err != nil {
		return StorageProfile{}, err
	}
	name, kind, cfg, credentials, err := normalizeStorageProfileInput(input, currentCredentials, true)
	if err != nil {
		return StorageProfile{}, err
	}
	var used int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM shares WHERE storage_profile_id=? AND status!='deleted'`, id).Scan(&used); err != nil {
		return StorageProfile{}, err
	}
	if used > 0 && (existing.Provider != input.Provider || existing.Kind != kind || existing.Config != cfg) {
		return StorageProfile{}, fmt.Errorf("%w: a referenced storage profile may only change its name or credentials; create a new profile for a new path, bucket, or provider", core.ErrConflict)
	}
	store, err := buildStorageStore(ctx, input.Provider, kind, cfg, credentials)
	if err != nil {
		return StorageProfile{}, fmt.Errorf("%w: %v", core.ErrValidation, err)
	}
	configJSON, _ := json.Marshal(cfg)
	encrypted := ""
	if kind == "s3" {
		encrypted, err = s.encryptStorageCredentials(id, credentials)
		if err != nil {
			return StorageProfile{}, err
		}
	}
	_, err = s.db.ExecContext(ctx, `UPDATE storage_profiles SET name=?,kind=?,provider=?,config_json=?,
		credentials_encrypted=?,last_checked_at=NULL,last_error=NULL,updated_at=? WHERE id=?`,
		name, kind, input.Provider, string(configJSON), encrypted, s.now().Unix(), id)
	if err != nil {
		return StorageProfile{}, err
	}
	s.storesMutex.Lock()
	s.stores[id] = store
	s.storesMutex.Unlock()
	return s.storageProfile(ctx, id)
}

func (s *Service) DeleteStorageProfile(ctx context.Context, id string) error {
	defaultID, _ := s.DefaultStorage(ctx)
	if id == defaultID || id == "local" {
		return fmt.Errorf("%w: default or built-in local storage cannot be deleted", core.ErrConflict)
	}
	var used int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM shares WHERE storage_profile_id=? AND status!='deleted'`, id).Scan(&used); err != nil {
		return err
	}
	if used > 0 {
		return fmt.Errorf("%w: storage profile is still referenced by shares", core.ErrConflict)
	}
	result, err := s.db.ExecContext(ctx, "DELETE FROM storage_profiles WHERE id=?", id)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return core.ErrNotFound
	}
	s.storesMutex.Lock()
	delete(s.stores, id)
	s.storesMutex.Unlock()
	return nil
}

func (s *Service) TestStorageProfile(ctx context.Context, id string) StorageHealth {
	result := s.CheckStorage(ctx, id)
	now := s.now().Unix()
	lastError := any(nil)
	if result.Error != "" {
		lastError = result.Error
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE storage_profiles SET last_checked_at=?,last_error=? WHERE id=?`, now, lastError, id)
	return result
}

func (s *Service) TestStorageInput(ctx context.Context, input StorageProfileInput) StorageHealth {
	result := StorageHealth{Name: strings.TrimSpace(input.Name)}
	_, kind, cfg, credentials, err := normalizeStorageProfileInput(input, StorageCredentials{}, false)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	store, err := buildStorageStore(ctx, input.Provider, kind, cfg, credentials)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	return checkStore(ctx, result.Name, store)
}

func (s *Service) StorageKind(ctx context.Context, profileID string) (string, error) {
	var kind string
	if err := s.db.QueryRowContext(ctx, "SELECT kind FROM storage_profiles WHERE id=? AND enabled=1", profileID).Scan(&kind); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", core.ErrNotFound
		}
		return "", err
	}
	return kind, nil
}

func (s *Service) storageProfile(ctx context.Context, id string) (StorageProfile, error) {
	record, err := s.storageProfileRecord(ctx, id)
	if err != nil {
		return StorageProfile{}, err
	}
	defaultID, _ := s.DefaultStorage(ctx)
	record.Default = id == defaultID
	if err := s.setStorageCredentialMetadata(&record); err != nil {
		return StorageProfile{}, err
	}
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM shares WHERE storage_profile_id=? AND status!='deleted'`, id).Scan(&record.InUse)
	return record.StorageProfile, nil
}

func (s *Service) setStorageCredentialMetadata(record *storageProfileRecord) error {
	if record.Kind != "s3" {
		return nil
	}
	credentials, err := s.decryptStorageCredentials(record.ID, record.credentialsEncrypted)
	if err != nil {
		return fmt.Errorf("decrypt storage profile %s: %w", record.ID, err)
	}
	record.HasCredentials = credentials.AccessKeyID != "" && credentials.SecretAccessKey != ""
	record.UsesDefaultCredentials = record.Provider == "aws-s3" && !record.HasCredentials
	record.HasSessionToken = credentials.SessionToken != ""
	return nil
}

func (s *Service) storageProfileRecord(ctx context.Context, id string) (storageProfileRecord, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,name,kind,provider,config_json,
		credentials_encrypted,enabled,last_checked_at,COALESCE(last_error,''),created_at,updated_at
		FROM storage_profiles WHERE id=?`, id)
	record, err := scanStorageProfile(row)
	if errors.Is(err, sql.ErrNoRows) {
		return storageProfileRecord{}, core.ErrNotFound
	}
	return record, err
}

type profileScanner interface{ Scan(...any) error }

func scanStorageProfile(scanner profileScanner) (storageProfileRecord, error) {
	var result storageProfileRecord
	var configJSON string
	var enabled int
	var checked sql.NullInt64
	var created, updated int64
	if err := scanner.Scan(&result.ID, &result.Name, &result.Kind, &result.Provider, &configJSON,
		&result.credentialsEncrypted, &enabled, &checked, &result.LastError, &created, &updated); err != nil {
		return storageProfileRecord{}, err
	}
	if err := json.Unmarshal([]byte(configJSON), &result.Config); err != nil {
		return storageProfileRecord{}, err
	}
	result.Enabled = enabled == 1
	if checked.Valid {
		value := time.Unix(checked.Int64, 0).UTC()
		result.LastCheckedAt = &value
	}
	result.CreatedAt = time.Unix(created, 0).UTC()
	result.UpdatedAt = time.Unix(updated, 0).UTC()
	return result, nil
}

func scanStorageProfileWithUsage(scanner profileScanner) (storageProfileRecord, error) {
	var result storageProfileRecord
	var configJSON string
	var enabled int
	var checked sql.NullInt64
	var created, updated int64
	if err := scanner.Scan(&result.ID, &result.Name, &result.Kind, &result.Provider, &configJSON,
		&result.credentialsEncrypted, &enabled, &checked, &result.LastError, &created, &updated, &result.InUse); err != nil {
		return storageProfileRecord{}, err
	}
	if err := json.Unmarshal([]byte(configJSON), &result.Config); err != nil {
		return storageProfileRecord{}, err
	}
	result.Enabled = enabled == 1
	if checked.Valid {
		value := time.Unix(checked.Int64, 0).UTC()
		result.LastCheckedAt = &value
	}
	result.CreatedAt = time.Unix(created, 0).UTC()
	result.UpdatedAt = time.Unix(updated, 0).UTC()
	return result, nil
}

func (s *Service) encryptStorageCredentials(id string, credentials StorageCredentials) (string, error) {
	data, err := json.Marshal(credentials)
	if err != nil {
		return "", err
	}
	return security.EncryptSecret(s.storageMasterKey, data, "storage-profile:"+id)
}

func (s *Service) decryptStorageCredentials(id, encrypted string) (StorageCredentials, error) {
	if encrypted == "" {
		return StorageCredentials{}, nil
	}
	data, err := security.DecryptSecret(s.storageMasterKey, encrypted, "storage-profile:"+id)
	if err != nil {
		return StorageCredentials{}, err
	}
	var result StorageCredentials
	if err := json.Unmarshal(data, &result); err != nil {
		return StorageCredentials{}, err
	}
	return result, nil
}

func normalizeStorageProfileInput(input StorageProfileInput, current StorageCredentials, updating bool) (string, string, StorageProfileConfig, StorageCredentials, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" || len([]rune(name)) > 80 {
		return "", "", StorageProfileConfig{}, StorageCredentials{}, fmt.Errorf("%w: storage profile name must contain 1-80 characters", core.ErrValidation)
	}
	provider := strings.TrimSpace(input.Provider)
	kind := providerKind(provider)
	if kind == "" {
		return "", "", StorageProfileConfig{}, StorageCredentials{}, fmt.Errorf("%w: unsupported storage provider", core.ErrValidation)
	}
	cfg := input.Config
	credentials := input.Credentials
	credentials.AccessKeyID = strings.TrimSpace(credentials.AccessKeyID)
	credentials.SecretAccessKey = strings.TrimSpace(credentials.SecretAccessKey)
	credentials.SessionToken = strings.TrimSpace(credentials.SessionToken)
	if updating {
		if (credentials.AccessKeyID == "") != (credentials.SecretAccessKey == "") {
			return "", "", StorageProfileConfig{}, StorageCredentials{}, fmt.Errorf("%w: access key ID and secret access key must be changed together", core.ErrValidation)
		}
		if credentials.AccessKeyID == "" && credentials.SecretAccessKey == "" {
			credentials.AccessKeyID = current.AccessKeyID
			credentials.SecretAccessKey = current.SecretAccessKey
		}
		if credentials.ClearSessionToken && credentials.SessionToken != "" {
			return "", "", StorageProfileConfig{}, StorageCredentials{}, fmt.Errorf("%w: session token cannot be replaced and cleared in the same request", core.ErrValidation)
		}
		if credentials.ClearSessionToken {
			credentials.SessionToken = ""
		} else if credentials.SessionToken == "" {
			credentials.SessionToken = current.SessionToken
		}
	} else if credentials.ClearSessionToken {
		return "", "", StorageProfileConfig{}, StorageCredentials{}, fmt.Errorf("%w: no saved session token exists on a new profile", core.ErrValidation)
	}
	credentials.ClearSessionToken = false
	if kind == "local" {
		cfg.RootPath = filepath.Clean(strings.TrimSpace(cfg.RootPath))
		if cfg.RootPath == "." || !filepath.IsAbs(cfg.RootPath) {
			return "", "", StorageProfileConfig{}, StorageCredentials{}, fmt.Errorf("%w: local or NAS root must be an absolute path", core.ErrValidation)
		}
		return name, kind, StorageProfileConfig{RootPath: cfg.RootPath}, StorageCredentials{}, nil
	}
	cfg.Bucket = strings.TrimSpace(cfg.Bucket)
	cfg.Region = strings.TrimSpace(cfg.Region)
	cfg.Prefix = strings.Trim(strings.ReplaceAll(strings.TrimSpace(cfg.Prefix), "\\", "/"), "/")
	if cfg.Prefix != "" {
		cleanedPrefix := path.Clean(cfg.Prefix)
		if cleanedPrefix == "." || cleanedPrefix == ".." || strings.HasPrefix(cleanedPrefix, "../") || strings.Contains(cleanedPrefix, ":") {
			return "", "", StorageProfileConfig{}, StorageCredentials{}, fmt.Errorf("%w: object prefix must not escape its storage namespace", core.ErrValidation)
		}
		cfg.Prefix = cleanedPrefix
	}
	cfg.AccountID = strings.TrimSpace(cfg.AccountID)
	cfg.Jurisdiction = strings.ToLower(strings.TrimSpace(cfg.Jurisdiction))
	cfg.Endpoint = strings.TrimRight(strings.TrimSpace(cfg.Endpoint), "/")
	if cfg.Bucket == "" || len(cfg.Bucket) > 255 {
		return "", "", StorageProfileConfig{}, StorageCredentials{}, fmt.Errorf("%w: bucket is required", core.ErrValidation)
	}
	switch provider {
	case "cloudflare-r2":
		if !accountIDPattern.MatchString(cfg.AccountID) {
			return "", "", StorageProfileConfig{}, StorageCredentials{}, fmt.Errorf("%w: Cloudflare account ID must contain 32 hexadecimal characters", core.ErrValidation)
		}
		if cfg.Jurisdiction != "" && cfg.Jurisdiction != "eu" && cfg.Jurisdiction != "fedramp" {
			return "", "", StorageProfileConfig{}, StorageCredentials{}, fmt.Errorf("%w: unsupported R2 jurisdiction", core.ErrValidation)
		}
		suffix := ""
		if cfg.Jurisdiction != "" {
			suffix = "." + cfg.Jurisdiction
		}
		cfg.Endpoint = "https://" + cfg.AccountID + suffix + ".r2.cloudflarestorage.com"
		cfg.Region, cfg.PathStyle = "auto", true
		if cfg.ServerSideEncryption == "" {
			cfg.ServerSideEncryption = "none"
		}
	case "aws-s3":
		cfg.Endpoint, cfg.PathStyle = "", false
		if cfg.Region == "" {
			return "", "", StorageProfileConfig{}, StorageCredentials{}, fmt.Errorf("%w: AWS region is required", core.ErrValidation)
		}
		if cfg.ServerSideEncryption == "" {
			cfg.ServerSideEncryption = "AES256"
		}
	case "backblaze-b2":
		if cfg.Region == "" {
			return "", "", StorageProfileConfig{}, StorageCredentials{}, fmt.Errorf("%w: Backblaze region is required", core.ErrValidation)
		}
		cfg.Endpoint, cfg.PathStyle = "https://s3."+cfg.Region+".backblazeb2.com", true
	case "wasabi":
		if cfg.Region == "" {
			return "", "", StorageProfileConfig{}, StorageCredentials{}, fmt.Errorf("%w: Wasabi region is required", core.ErrValidation)
		}
		cfg.Endpoint, cfg.PathStyle = "https://s3."+cfg.Region+".wasabisys.com", false
	case "digitalocean-spaces":
		if cfg.Region == "" {
			return "", "", StorageProfileConfig{}, StorageCredentials{}, fmt.Errorf("%w: Spaces region is required", core.ErrValidation)
		}
		cfg.Endpoint, cfg.PathStyle = "https://"+cfg.Region+".digitaloceanspaces.com", false
	case "minio", "generic-s3":
		if cfg.Endpoint == "" || cfg.Region == "" {
			return "", "", StorageProfileConfig{}, StorageCredentials{}, fmt.Errorf("%w: endpoint and region are required", core.ErrValidation)
		}
	}
	if cfg.Endpoint != "" {
		parsed, err := url.Parse(cfg.Endpoint)
		if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" || parsed.User != nil ||
			(parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
			return "", "", StorageProfileConfig{}, StorageCredentials{}, fmt.Errorf("%w: storage endpoint must be an HTTP(S) origin", core.ErrValidation)
		}
	}
	if cfg.ServerSideEncryption == "" {
		cfg.ServerSideEncryption = "none"
	}
	if cfg.ServerSideEncryption != "none" && cfg.ServerSideEncryption != "AES256" && cfg.ServerSideEncryption != "aws:kms" {
		return "", "", StorageProfileConfig{}, StorageCredentials{}, fmt.Errorf("%w: unsupported server-side encryption", core.ErrValidation)
	}
	if !providerSupportsServerSideEncryption(provider, cfg.ServerSideEncryption) {
		return "", "", StorageProfileConfig{}, StorageCredentials{}, fmt.Errorf("%w: %s does not support the selected server-side encryption mode", core.ErrValidation, provider)
	}
	usesAWSDefaultCredentials := provider == "aws-s3" && credentials.AccessKeyID == "" &&
		credentials.SecretAccessKey == "" && credentials.SessionToken == ""
	if !usesAWSDefaultCredentials && (credentials.AccessKeyID == "" || credentials.SecretAccessKey == "") {
		return "", "", StorageProfileConfig{}, StorageCredentials{}, fmt.Errorf("%w: access key ID and secret access key are required", core.ErrValidation)
	}
	return name, kind, cfg, credentials, nil
}

func providerSupportsServerSideEncryption(provider, mode string) bool {
	switch provider {
	case "cloudflare-r2", "digitalocean-spaces":
		return mode == "none"
	case "backblaze-b2", "wasabi":
		return mode == "none" || mode == "AES256"
	case "aws-s3", "minio", "generic-s3":
		return mode == "none" || mode == "AES256" || mode == "aws:kms"
	default:
		return false
	}
}

func buildStorageStore(ctx context.Context, provider, kind string, cfg StorageProfileConfig, credentials StorageCredentials) (storage.Store, error) {
	if kind == "local" {
		return storage.NewLocal(cfg.RootPath)
	}
	return storage.NewS3(ctx, storage.S3Options{
		Endpoint: cfg.Endpoint, Region: cfg.Region, Bucket: cfg.Bucket, Prefix: cfg.Prefix,
		PathStyle: cfg.PathStyle, ServerSideEncryption: cfg.ServerSideEncryption,
		AccessKeyID: credentials.AccessKeyID, SecretAccessKey: credentials.SecretAccessKey,
		SessionToken: credentials.SessionToken,
	})
}

func providerKind(provider string) string {
	switch provider {
	case "local", "nas":
		return "local"
	case "cloudflare-r2", "aws-s3", "backblaze-b2", "wasabi", "digitalocean-spaces", "minio", "generic-s3":
		return "s3"
	default:
		return ""
	}
}

func detectLegacyS3Provider(endpoint string) string {
	lower := strings.ToLower(strings.TrimSpace(endpoint))
	if lower == "" {
		return "aws-s3"
	}
	if strings.Contains(lower, ".r2.cloudflarestorage.com") {
		return "cloudflare-r2"
	}
	if strings.Contains(lower, ".backblazeb2.com") {
		return "backblaze-b2"
	}
	if strings.Contains(lower, ".wasabisys.com") {
		return "wasabi"
	}
	if strings.Contains(lower, ".digitaloceanspaces.com") {
		return "digitalocean-spaces"
	}
	return "generic-s3"
}

func storageProviderLabel(provider string) string {
	for _, definition := range StorageProviderCatalog() {
		if definition.ID == provider {
			return definition.Label
		}
	}
	return "S3 兼容存储"
}

func parseR2Endpoint(endpoint string) (string, string) {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", ""
	}
	parts := strings.Split(strings.ToLower(parsed.Hostname()), ".")
	if len(parts) < 4 || !accountIDPattern.MatchString(parts[0]) {
		return "", ""
	}
	jurisdiction := ""
	if parts[1] == "eu" || parts[1] == "fedramp" {
		jurisdiction = parts[1]
	}
	return parts[0], jurisdiction
}
