package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ei4869/SharePipe-OSS/internal/config"
	"github.com/ei4869/SharePipe-OSS/internal/core"
	"github.com/ei4869/SharePipe-OSS/internal/database"
	"github.com/ei4869/SharePipe-OSS/internal/scanner"
	"github.com/ei4869/SharePipe-OSS/internal/security"
	"github.com/ei4869/SharePipe-OSS/internal/storage"
)

type Options struct {
	Database          *database.Database
	Config            config.Config
	Stores            map[string]storage.Store
	Signer            *security.Signer
	AdminPasswordHash string
	StorageMasterKey  []byte
	StagingRoot       string
	AuditSalt         []byte
	Scanner           scanner.Scanner
}

type Service struct {
	db                *database.Database
	cfg               config.Config
	stores            map[string]storage.Store
	storesMutex       sync.RWMutex
	signer            *security.Signer
	adminPasswordHash atomic.Value
	storageMasterKey  []byte
	stagingRoot       string
	auditSalt         []byte
	scanner           scanner.Scanner
	cloudAdmin        *cloudAdminClient
	runtime           atomic.Value
	runtimeMutex      sync.Mutex
	now               func() time.Time
}

func New(options Options) (*Service, error) {
	if options.Database == nil || options.Signer == nil {
		return nil, errors.New("database and session signer are required")
	}
	if len(options.StorageMasterKey) != 32 {
		return nil, errors.New("storage master key must contain exactly 32 bytes")
	}
	if len(options.AuditSalt) < 16 {
		return nil, errors.New("audit salt must contain at least 16 bytes")
	}
	if err := os.MkdirAll(options.StagingRoot, 0o750); err != nil {
		return nil, fmt.Errorf("create upload staging root: %w", err)
	}
	result := &Service{
		db: options.Database, cfg: options.Config, stores: make(map[string]storage.Store), signer: options.Signer,
		storageMasterKey: append([]byte(nil), options.StorageMasterKey...), stagingRoot: filepath.Clean(options.StagingRoot),
		auditSalt: append([]byte(nil), options.AuditSalt...), scanner: options.Scanner, now: time.Now,
		cloudAdmin: newCloudAdminClient(options.Config.Admin.CloudControlURL, options.Config.CloudControlToken),
	}
	result.adminPasswordHash.Store("")
	if result.scanner == nil {
		result.scanner = scanner.Noop{}
	}
	if err := result.initializeAdministrator(context.Background(), options.AdminPasswordHash); err != nil {
		return nil, fmt.Errorf("initialize administrator: %w", err)
	}
	if err := result.initializeStorageProfiles(context.Background()); err != nil {
		return nil, fmt.Errorf("initialize storage profiles: %w", err)
	}
	if err := result.initializeRuntimeSettings(context.Background()); err != nil {
		return nil, fmt.Errorf("initialize runtime settings: %w", err)
	}
	return result, nil
}

func (s *Service) Signer() *security.Signer { return s.signer }

func (s *Service) Config() config.Config { return s.cfg }

func (s *Service) DefaultStorage(ctx context.Context) (string, error) {
	var profileID string
	if err := s.db.QueryRowContext(ctx, "SELECT value FROM settings WHERE key='default_storage_profile'").Scan(&profileID); err != nil {
		return "", err
	}
	if _, err := s.Store(profileID); err != nil {
		return "", fmt.Errorf("%w: configured storage profile %s is unavailable", core.ErrConflict, profileID)
	}
	return profileID, nil
}

func (s *Service) SetDefaultStorage(ctx context.Context, profileID string) error {
	if _, err := s.Store(profileID); err != nil {
		return fmt.Errorf("%w: storage profile %s is unavailable", core.ErrValidation, profileID)
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO settings(key,value,updated_at)
		VALUES('default_storage_profile',?,?) ON CONFLICT(key) DO UPDATE SET
		value=excluded.value, updated_at=excluded.updated_at`, profileID, s.now().Unix())
	return err
}

func (s *Service) AuthenticateAdmin(password string) bool {
	hash, _ := s.adminPasswordHash.Load().(string)
	if hash == "" {
		return false
	}
	ok, err := security.VerifyPassword(hash, password)
	return err == nil && ok
}

func (s *Service) Store(name string) (storage.Store, error) {
	s.storesMutex.RLock()
	store := s.stores[name]
	s.storesMutex.RUnlock()
	if store == nil {
		return nil, fmt.Errorf("%w: storage backend %s", core.ErrNotFound, name)
	}
	return store, nil
}

func (s *Service) storeFor(profileID, legacyBackend string) (storage.Store, error) {
	if strings.TrimSpace(profileID) == "" {
		profileID = legacyBackend
	}
	return s.Store(profileID)
}

func (s *Service) passwordVersion(ctx context.Context) int {
	var raw string
	if err := s.db.QueryRowContext(ctx, "SELECT value FROM settings WHERE key='admin_password_version'").Scan(&raw); err != nil {
		return 1
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 {
		return 1
	}
	return value
}

func (s *Service) AdminPasswordVersion(ctx context.Context) int { return s.passwordVersion(ctx) }

func (s *Service) Audit(ctx context.Context, shareID, action, result, remoteIP, userAgent, details string) {
	s.audit(ctx, shareID, action, result, remoteIP, userAgent, details)
}

func (s *Service) audit(ctx context.Context, shareID, action, result, remoteIP, userAgent, details string) {
	var digest string
	if remoteIP != "" {
		mac := hmac.New(sha256.New, s.auditSalt)
		_, _ = mac.Write([]byte(remoteIP))
		digest = hex.EncodeToString(mac.Sum(nil))
	}
	diagnostic := s.diagnosticEnabled(ctx)
	var fullIP, fullAgent sql.NullString
	if diagnostic {
		fullIP = sql.NullString{String: remoteIP, Valid: remoteIP != ""}
		fullAgent = sql.NullString{String: userAgent, Valid: userAgent != ""}
	}
	_, _ = s.db.ExecContext(ctx, `INSERT INTO audit_logs(
		share_id, action, result, ip_digest, full_ip, user_agent, details, created_at
	) VALUES(NULLIF(?, ''), ?, ?, NULLIF(?, ''), ?, ?, NULLIF(?, ''), ?)`,
		shareID, action, result, digest, fullIP, fullAgent, details, s.now().Unix())
}

func (s *Service) diagnosticEnabled(ctx context.Context) bool {
	var raw string
	if err := s.db.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = 'diagnostic_until'").Scan(&raw); err != nil {
		return false
	}
	var unix int64
	if _, err := fmt.Sscanf(raw, "%d", &unix); err != nil {
		return false
	}
	return unix > s.now().Unix()
}

func nullableTime(value sql.NullInt64) *time.Time {
	if !value.Valid {
		return nil
	}
	result := time.Unix(value.Int64, 0).UTC()
	return &result
}

func nullableInt(value sql.NullInt64) *int {
	if !value.Valid {
		return nil
	}
	result := int(value.Int64)
	return &result
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}
