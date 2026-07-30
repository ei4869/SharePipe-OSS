package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"mime"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/ei4869/SharePipe-OSS/internal/core"
	"github.com/ei4869/SharePipe-OSS/internal/security"
	"github.com/ei4869/SharePipe-OSS/internal/storage"
)

type CreateShareInput struct {
	InviteCode      string
	Name            string
	Description     string
	ExpiresIn       string
	DownloadLimit   *int
	Password        string
	Entries         []core.ManifestEntry
	TrustedDeviceID string
	LocalSystem     bool
	PairingEnabled  bool
	QREnabled       bool
	RemoteIP        string
	UserAgent       string
	Platform        string
}

type preparedEntry struct {
	core.ManifestEntry
	ID         string
	StorageKey string
}

func (s *Service) CreateShare(ctx context.Context, input CreateShareInput) (core.ShareCreation, error) {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || len([]rune(input.Name)) > 120 {
		return core.ShareCreation{}, fmt.Errorf("%w: share name must contain 1-120 characters", core.ErrValidation)
	}
	if len([]rune(input.Description)) > 1000 {
		return core.ShareCreation{}, fmt.Errorf("%w: description is too long", core.ErrValidation)
	}
	runtimeSettings := s.RuntimeSettings()
	expiryValue := input.ExpiresIn
	if expiryValue == "" {
		expiryValue = runtimeSettings.DefaultExpiry
	}
	expiryDuration, ok := s.cfg.ExpiryAllowed(expiryValue)
	if !ok {
		return core.ShareCreation{}, fmt.Errorf("%w: unsupported expiry", core.ErrValidation)
	}
	entries, fileCount, totalBytes, err := s.prepareManifest(input.Entries, runtimeSettings)
	if err != nil {
		return core.ShareCreation{}, err
	}
	profileID, err := s.DefaultStorage(ctx)
	if err != nil {
		return core.ShareCreation{}, err
	}
	backend, err := s.StorageKind(ctx, profileID)
	if err != nil {
		return core.ShareCreation{}, err
	}
	var limit *int
	if input.DownloadLimit == nil {
		if runtimeSettings.DefaultDownloadLimit > 0 {
			value := runtimeSettings.DefaultDownloadLimit
			limit = &value
		}
	} else if *input.DownloadLimit < 0 {
		return core.ShareCreation{}, fmt.Errorf("%w: download limit cannot be negative", core.ErrValidation)
	} else if *input.DownloadLimit > 0 {
		value := *input.DownloadLimit
		limit = &value
	}
	var passwordHash any
	if input.Password != "" {
		hash, err := security.HashPassword(input.Password)
		if err != nil {
			return core.ShareCreation{}, fmt.Errorf("%w: %v", core.ErrValidation, err)
		}
		passwordHash = hash
	}

	shareID, _, err := security.RandomToken(16)
	if err != nil {
		return core.ShareCreation{}, err
	}
	publicToken, publicDigest, err := security.RandomToken(32)
	if err != nil {
		return core.ShareCreation{}, err
	}
	manageToken, manageDigest, err := security.RandomToken(32)
	if err != nil {
		return core.ShareCreation{}, err
	}
	encryptedPublicToken, err := security.EncryptSecret(s.storageMasterKey, []byte(publicToken), "public-token:"+shareID)
	if err != nil {
		return core.ShareCreation{}, fmt.Errorf("encrypt public share token: %w", err)
	}
	for index := range entries {
		entryID, _, err := security.RandomToken(16)
		if err != nil {
			return core.ShareCreation{}, err
		}
		entries[index].ID = entryID
		if entries[index].Kind == "file" {
			entries[index].StorageKey = storage.ObjectKey(shareID, entryID)
		}
	}

	now := s.now().UTC()
	expiresAt := now.Add(expiryDuration)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return core.ShareCreation{}, err
	}
	defer tx.Rollback()
	var invite core.Invite
	var inviteID any
	trustedDeviceID := input.TrustedDeviceID
	trustedToken := ""
	trustedName := ""
	if input.LocalSystem {
		trustedDeviceID = "local-system"
	} else if trustedDeviceID != "" {
		var active string
		if err := tx.QueryRowContext(ctx, "SELECT id FROM trusted_devices WHERE id=? AND revoked_at IS NULL AND expires_at>?", trustedDeviceID, now.Unix()).Scan(&active); err != nil {
			return core.ShareCreation{}, core.ErrUnauthorized
		}
	} else {
		invite, err = consumeInvite(ctx, tx, input.InviteCode, now)
		if err != nil {
			return core.ShareCreation{}, err
		}
		inviteID = invite.ID
		if invite.Kind == "trusted_device" {
			var digest string
			trustedToken, digest, err = newTrustedCredential()
			if err != nil {
				return core.ShareCreation{}, err
			}
			trustedDeviceID, _, err = security.RandomToken(16)
			if err != nil {
				return core.ShareCreation{}, err
			}
			trustedName = invite.DeviceName
			expires := now.Add(trustedDeviceTTL).Unix()
			ipDigest := s.trustedIPDigest(input.RemoteIP)
			_, err = tx.ExecContext(ctx, `INSERT INTO trusted_devices(id,credential_hash,name,source_invite_id,platform,user_agent,first_ip_digest,last_ip_digest,expires_at,last_seen_at,created_at,updated_at)
				VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, trustedDeviceID, digest, trustedName, invite.ID, input.Platform, input.UserAgent, ipDigest, ipDigest, expires, now.Unix(), now.Unix(), now.Unix())
			if err != nil {
				return core.ShareCreation{}, err
			}
		}
	}
	var downloadLimit any
	if limit != nil {
		downloadLimit = *limit
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO shares(
		id, public_token_hash, public_token_encrypted, manage_token_hash, name, description, password_hash,
		storage_backend, storage_profile_id, status, expires_at, download_limit, file_count, total_bytes,
		invite_id,trusted_device_id,pairing_enabled,qr_enabled,
		created_at, updated_at
	) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, 'uploading', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		shareID, publicDigest, encryptedPublicToken, manageDigest, input.Name, input.Description, passwordHash,
		backend, profileID, expiresAt.Unix(), downloadLimit, fileCount, totalBytes, inviteID, nullableString(trustedDeviceID), input.PairingEnabled, input.QREnabled,
		now.Unix(), now.Unix())
	if err != nil {
		return core.ShareCreation{}, fmt.Errorf("insert share: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO upload_tasks(share_id,status,created_at,updated_at)
		VALUES(?,'uploading',?,?)`, shareID, now.Unix(), now.Unix()); err != nil {
		return core.ShareCreation{}, fmt.Errorf("insert upload task: %w", err)
	}

	uploads := make([]core.UploadDescriptor, 0, fileCount)
	for _, entry := range entries {
		status := "ready"
		var storageKey any
		if entry.Kind == "file" {
			status = "pending"
			storageKey = entry.StorageKey
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO entries(
			id, share_id, path, kind, size, mime_type, storage_key, status, created_at
		) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			entry.ID, shareID, entry.Path, entry.Kind, entry.Size, entry.MIMEType, storageKey, status, now.Unix())
		if err != nil {
			return core.ShareCreation{}, fmt.Errorf("insert manifest entry: %w", err)
		}
		if entry.Kind != "file" {
			continue
		}
		sessionID, _, err := security.RandomToken(16)
		if err != nil {
			return core.ShareCreation{}, err
		}
		uploadToken, uploadDigest, err := security.RandomToken(32)
		if err != nil {
			return core.ShareCreation{}, err
		}
		partCount := 0
		if entry.Size > 0 {
			partCount = int((entry.Size + s.cfg.Uploads.ChunkSizeBytes - 1) / s.cfg.Uploads.ChunkSizeBytes)
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO upload_sessions(
			id, share_id, entry_id, token_hash, backend, storage_profile_id, expected_size, chunk_size,
			expected_parts, status, expires_at, created_at, updated_at
		) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, 'open', ?, ?, ?)`,
			sessionID, shareID, entry.ID, uploadDigest, backend, profileID,
			entry.Size, s.cfg.Uploads.ChunkSizeBytes, partCount,
			now.Add(time.Duration(s.cfg.Uploads.SessionTTLMinutes)*time.Minute).Unix(), now.Unix(), now.Unix())
		if err != nil {
			return core.ShareCreation{}, fmt.Errorf("insert upload session: %w", err)
		}
		uploads = append(uploads, core.UploadDescriptor{
			SessionID: sessionID, EntryID: entry.ID, Path: entry.Path, Token: uploadToken,
			Backend: backend, ChunkSize: s.cfg.Uploads.ChunkSizeBytes, PartCount: partCount,
			ExpectedSize: entry.Size, Status: "open", Parts: []core.UploadPart{},
		})
	}
	if err := tx.Commit(); err != nil {
		return core.ShareCreation{}, fmt.Errorf("commit share: %w", err)
	}

	if backend == "s3" {
		if err := s.initializeS3Uploads(ctx, profileID, shareID, entries, uploads); err != nil {
			_, _ = s.db.ExecContext(ctx, `UPDATE shares SET status='cleanup_pending', revoked_at=?, updated_at=? WHERE id=?`,
				now.Unix(), now.Unix(), shareID)
			return core.ShareCreation{}, err
		}
	}
	share := core.Share{
		ID: shareID, Name: input.Name, Description: input.Description,
		StorageBackend: backend, StorageProfileID: profileID, Status: "uploading",
		ExpiresAt: expiresAt, DownloadLimit: limit, FileCount: fileCount, TotalBytes: totalBytes,
		PasswordProtected: input.Password != "", CreatedAt: now, UpdatedAt: now,
		PairingEnabled: input.PairingEnabled, QREnabled: input.QREnabled,
	}
	base := strings.TrimRight(s.cfg.Server.PublicBaseURL, "/")
	return core.ShareCreation{
		Share: share, PublicToken: publicToken, ManagementToken: manageToken,
		PublicURL:          base + "/s/" + url.PathEscape(publicToken),
		ManagementURL:      base + "/m/" + url.PathEscape(manageToken),
		RecoveryURL:        base + "/resume/" + url.PathEscape(manageToken),
		UploadTaskStatus:   "uploading",
		Uploads:            uploads,
		TrustedDeviceToken: trustedToken, TrustedDeviceName: trustedName,
	}, nil
}

func (s *Service) initializeS3Uploads(ctx context.Context, profileID, shareID string, entries []preparedEntry, uploads []core.UploadDescriptor) error {
	store, err := s.Store(profileID)
	if err != nil {
		return err
	}
	multipart, ok := store.(storage.MultipartStore)
	if !ok {
		return errors.New("configured S3 store does not support multipart uploads")
	}
	byEntry := make(map[string]preparedEntry, len(entries))
	for _, entry := range entries {
		byEntry[entry.ID] = entry
	}
	started := make([]struct{ key, uploadID string }, 0)
	for index := range uploads {
		entry := byEntry[uploads[index].EntryID]
		if entry.Size == 0 {
			continue
		}
		uploadID, err := multipart.CreateMultipart(ctx, entry.StorageKey, entry.MIMEType)
		if err != nil {
			for _, item := range started {
				_ = multipart.AbortMultipart(ctx, item.key, item.uploadID)
			}
			return fmt.Errorf("initialize S3 upload for %s: %w", entry.Path, err)
		}
		started = append(started, struct{ key, uploadID string }{entry.StorageKey, uploadID})
		uploads[index].UploadID = uploadID
		if _, err := s.db.ExecContext(ctx, "UPDATE upload_sessions SET s3_upload_id=?, updated_at=? WHERE id=?",
			uploadID, s.now().Unix(), uploads[index].SessionID); err != nil {
			return fmt.Errorf("record S3 multipart upload: %w", err)
		}
	}
	return nil
}

func (s *Service) prepareManifest(input []core.ManifestEntry, limits RuntimeSettings) ([]preparedEntry, int, int64, error) {
	if len(input) == 0 {
		return nil, 0, 0, fmt.Errorf("%w: at least one file is required", core.ErrValidation)
	}
	seen := make(map[string]preparedEntry)
	fileCount := 0
	var totalBytes int64
	for _, item := range input {
		normalized, err := core.NormalizeRelativePath(item.Path)
		if err != nil {
			return nil, 0, 0, err
		}
		if item.Kind != "file" && item.Kind != "directory" {
			return nil, 0, 0, fmt.Errorf("%w: entry kind must be file or directory", core.ErrValidation)
		}
		if _, exists := seen[normalized]; exists {
			return nil, 0, 0, fmt.Errorf("%w: duplicate path %s", core.ErrValidation, normalized)
		}
		if item.Kind == "directory" {
			item.Size = 0
			item.MIMEType = "inode/directory"
		} else {
			if item.Size < 0 || item.Size > limits.MaxFileBytes {
				return nil, 0, 0, fmt.Errorf("%w: file %s exceeds the size limit", core.ErrValidation, normalized)
			}
			fileCount++
			totalBytes += item.Size
			if totalBytes > limits.MaxShareBytes {
				return nil, 0, 0, fmt.Errorf("%w: share exceeds the total size limit", core.ErrValidation)
			}
			item.MIMEType = normalizeMIME(item.MIMEType)
			for _, parent := range core.ParentDirectories(normalized) {
				if existing, ok := seen[parent]; ok && existing.Kind != "directory" {
					return nil, 0, 0, fmt.Errorf("%w: file conflicts with directory %s", core.ErrValidation, parent)
				}
				if _, ok := seen[parent]; !ok {
					seen[parent] = preparedEntry{ManifestEntry: core.ManifestEntry{Path: parent, Kind: "directory", MIMEType: "inode/directory"}}
				}
			}
		}
		item.Path = normalized
		seen[normalized] = preparedEntry{ManifestEntry: item}
	}
	if fileCount == 0 {
		return nil, 0, 0, fmt.Errorf("%w: at least one file is required", core.ErrValidation)
	}
	if fileCount > limits.MaxFilesPerShare {
		return nil, 0, 0, fmt.Errorf("%w: share has too many files", core.ErrValidation)
	}
	result := make([]preparedEntry, 0, len(seen))
	for _, entry := range seen {
		result = append(result, entry)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Path == result[j].Path {
			return result[i].Kind == "directory"
		}
		return result[i].Path < result[j].Path
	})
	return result, fileCount, totalBytes, nil
}

func normalizeMIME(raw string) string {
	raw = strings.TrimSpace(strings.Split(raw, ";")[0])
	if raw == "" || len(raw) > 255 {
		return "application/octet-stream"
	}
	if _, _, err := mime.ParseMediaType(raw); err != nil {
		return "application/octet-stream"
	}
	return raw
}

func (s *Service) GetPublicShare(ctx context.Context, token string, unlocked bool) (core.PublicShare, error) {
	share, passwordHash, err := s.findShare(ctx, "public_token_hash", security.HashToken(token))
	if err != nil {
		return core.PublicShare{}, err
	}
	if err := s.enforceReadableShare(ctx, &share); err != nil {
		return core.PublicShare{}, err
	}
	if passwordHash.Valid && !unlocked {
		return core.PublicShare{Share: share, Locked: true}, nil
	}
	entries, err := s.listEntries(ctx, share.ID)
	if err != nil {
		return core.PublicShare{}, err
	}
	return core.PublicShare{Share: share, Entries: entries}, nil
}

func (s *Service) GetManagedShare(ctx context.Context, token string) (core.PublicShare, error) {
	share, _, err := s.findShare(ctx, "manage_token_hash", security.HashToken(token))
	if err != nil {
		return core.PublicShare{}, err
	}
	entries, err := s.listEntries(ctx, share.ID)
	if err != nil {
		return core.PublicShare{}, err
	}
	pairing, err := s.PairingForManagedShare(ctx, share.ID)
	if err != nil {
		return core.PublicShare{}, err
	}
	return core.PublicShare{Share: share, Entries: entries, Pairing: &pairing}, nil
}

func (s *Service) VerifySharePassword(ctx context.Context, token, password string) (core.Share, error) {
	share, hash, err := s.findShare(ctx, "public_token_hash", security.HashToken(token))
	if err != nil {
		return core.Share{}, err
	}
	if err := s.enforceReadableShare(ctx, &share); err != nil {
		return core.Share{}, err
	}
	if !hash.Valid {
		return share, nil
	}
	ok, err := security.VerifyPassword(hash.String, password)
	if err != nil || !ok {
		return core.Share{}, core.ErrUnauthorized
	}
	return share, nil
}

func (s *Service) SharePasswordVersion(ctx context.Context, shareID string) (int, error) {
	var version int
	err := s.db.QueryRowContext(ctx, "SELECT password_version FROM shares WHERE id=? AND status!='deleted'", shareID).Scan(&version)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, core.ErrNotFound
	}
	return version, err
}

func (s *Service) findShare(ctx context.Context, tokenColumn, digest string) (core.Share, sql.NullString, error) {
	if tokenColumn != "public_token_hash" && tokenColumn != "manage_token_hash" {
		return core.Share{}, sql.NullString{}, errors.New("unsupported token lookup")
	}
	query := `SELECT id, name, description, storage_backend, storage_profile_id, status, expires_at,
		download_limit, download_count, file_count, total_bytes, password_hash,
		content_deleted_at, metadata_delete_after, created_at, updated_at,pairing_enabled,qr_enabled
		FROM shares WHERE ` + tokenColumn + ` = ? AND status != 'deleted'`
	row := s.db.QueryRowContext(ctx, query, digest)
	share, password, err := scanShare(row)
	if !errors.Is(err, core.ErrNotFound) || tokenColumn != "public_token_hash" {
		return share, password, err
	}
	row = s.db.QueryRowContext(ctx, `SELECT s.id,s.name,s.description,s.storage_backend,s.storage_profile_id,s.status,s.expires_at,
		s.download_limit,s.download_count,s.file_count,s.total_bytes,s.password_hash,s.content_deleted_at,s.metadata_delete_after,
		s.created_at,s.updated_at,s.pairing_enabled,s.qr_enabled FROM share_access_tokens a JOIN shares s ON s.id=a.share_id
		WHERE a.token_hash=? AND a.expires_at>? AND s.status!='deleted'`, digest, s.now().Unix())
	return scanShare(row)
}

func (s *Service) listEntries(ctx context.Context, shareID string) ([]core.Entry, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, share_id, path, kind, size, mime_type,
		COALESCE(storage_key, ''), COALESCE(sha256, ''), status, created_at
		FROM entries WHERE share_id = ? ORDER BY path`, shareID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]core.Entry, 0)
	for rows.Next() {
		var entry core.Entry
		var created int64
		if err := rows.Scan(&entry.ID, &entry.ShareID, &entry.Path, &entry.Kind, &entry.Size,
			&entry.MIMEType, &entry.StorageKey, &entry.SHA256, &entry.Status, &created); err != nil {
			return nil, err
		}
		entry.CreatedAt = time.Unix(created, 0).UTC()
		result = append(result, entry)
	}
	return result, rows.Err()
}

func (s *Service) enforceReadableShare(ctx context.Context, share *core.Share) error {
	now := s.now().UTC()
	if (share.Status == "active" || share.Status == "uploading") && !share.ExpiresAt.After(now) {
		_, _ = s.db.ExecContext(ctx, "UPDATE shares SET status='expired', updated_at=? WHERE id=? AND status IN ('active','uploading')",
			now.Unix(), share.ID)
		share.Status = "expired"
		share.UpdatedAt = now
	}
	switch share.Status {
	case "active":
		return nil
	case "uploading":
		return core.ErrConflict
	case "expired":
		return core.ErrExpired
	default:
		return core.ErrNotFound
	}
}

func scanShare(scanner rowScanner) (core.Share, sql.NullString, error) {
	var share core.Share
	var expiresAt, createdAt, updatedAt int64
	var limit, contentDeleted, metadataDelete sql.NullInt64
	var passwordHash sql.NullString
	if err := scanner.Scan(
		&share.ID, &share.Name, &share.Description, &share.StorageBackend, &share.StorageProfileID, &share.Status,
		&expiresAt, &limit, &share.DownloadCount, &share.FileCount, &share.TotalBytes,
		&passwordHash, &contentDeleted, &metadataDelete, &createdAt, &updatedAt, &share.PairingEnabled, &share.QREnabled,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return core.Share{}, sql.NullString{}, core.ErrNotFound
		}
		return core.Share{}, sql.NullString{}, err
	}
	share.ExpiresAt = time.Unix(expiresAt, 0).UTC()
	share.DownloadLimit = nullableInt(limit)
	share.PasswordProtected = passwordHash.Valid
	share.ContentDeletedAt = nullableTime(contentDeleted)
	share.MetadataDeleteAfter = nullableTime(metadataDelete)
	share.CreatedAt = time.Unix(createdAt, 0).UTC()
	share.UpdatedAt = time.Unix(updatedAt, 0).UTC()
	return share, passwordHash, nil
}

type UpdateShareInput struct {
	ExpiresIn     *string
	DownloadLimit *int
	Unlimited     *bool
	Password      *string
}

func (s *Service) UpdateManagedShare(ctx context.Context, token string, input UpdateShareInput) (core.PublicShare, error) {
	share, currentHash, err := s.findShare(ctx, "manage_token_hash", security.HashToken(token))
	if err != nil {
		return core.PublicShare{}, err
	}
	if share.Status == "revoked" || share.Status == "metadata_only" || share.Status == "deleted" {
		return core.PublicShare{}, core.ErrConflict
	}
	expiresAt := share.ExpiresAt
	if input.ExpiresIn != nil {
		duration, ok := s.cfg.ExpiryAllowed(*input.ExpiresIn)
		if !ok {
			return core.PublicShare{}, fmt.Errorf("%w: unsupported expiry", core.ErrValidation)
		}
		expiresAt = s.now().Add(duration).UTC()
	}
	limit := share.DownloadLimit
	if input.Unlimited != nil && *input.Unlimited {
		limit = nil
	} else if input.DownloadLimit != nil {
		if *input.DownloadLimit < 1 || *input.DownloadLimit < share.DownloadCount {
			return core.PublicShare{}, fmt.Errorf("%w: download limit is below the current count", core.ErrValidation)
		}
		value := *input.DownloadLimit
		limit = &value
	}
	var password any
	if currentHash.Valid {
		password = currentHash.String
	}
	if input.Password != nil {
		if *input.Password == "" {
			password = nil
		} else {
			hash, err := security.HashPassword(*input.Password)
			if err != nil {
				return core.PublicShare{}, fmt.Errorf("%w: %v", core.ErrValidation, err)
			}
			password = hash
		}
	}
	var limitValue any
	if limit != nil {
		limitValue = *limit
	}
	status := share.Status
	if status == "expired" && share.ContentDeletedAt == nil {
		status = "active"
	}
	now := s.now().UTC()
	passwordVersionIncrement := 0
	if input.Password != nil {
		passwordVersionIncrement = 1
	}
	_, err = s.db.ExecContext(ctx, `UPDATE shares SET expires_at=?, download_limit=?, password_hash=?,
		password_version=password_version+?, status=?, updated_at=? WHERE id=?`, expiresAt.Unix(), limitValue, password,
		passwordVersionIncrement, status, now.Unix(), share.ID)
	if err != nil {
		return core.PublicShare{}, err
	}
	return s.GetManagedShare(ctx, token)
}

func (s *Service) RevokeManagedShare(ctx context.Context, token string) error {
	share, _, err := s.findShare(ctx, "manage_token_hash", security.HashToken(token))
	if err != nil {
		return err
	}
	now := s.now().Unix()
	result, err := s.db.ExecContext(ctx, `UPDATE shares SET status='revoked', revoked_at=?, updated_at=?
		WHERE id=? AND status NOT IN ('revoked','deleted')`, now, now, share.ID)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return core.ErrConflict
	}
	return nil
}

func (s *Service) RevokeShareByID(ctx context.Context, shareID string) error {
	now := s.now().Unix()
	result, err := s.db.ExecContext(ctx, `UPDATE shares SET status='revoked', revoked_at=?, updated_at=?
		WHERE id=? AND status NOT IN ('revoked','metadata_only','deleted')`, now, now, shareID)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected > 0 {
		return nil
	}
	var exists int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM shares WHERE id=?", shareID).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return core.ErrNotFound
	}
	return core.ErrConflict
}

func (s *Service) ListShares(ctx context.Context, limit, offset int) ([]core.Share, error) {
	if limit < 1 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, description, storage_backend, storage_profile_id, status, expires_at,
		download_limit, download_count, file_count, total_bytes, password_hash,
		content_deleted_at, metadata_delete_after, created_at, updated_at,pairing_enabled,qr_enabled
		FROM shares ORDER BY created_at DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]core.Share, 0)
	for rows.Next() {
		share, _, err := scanShare(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, share)
	}
	return result, rows.Err()
}
