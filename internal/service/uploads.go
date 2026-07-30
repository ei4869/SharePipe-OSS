package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ei4869/SharePipe-OSS/internal/core"
	"github.com/ei4869/SharePipe-OSS/internal/scanner"
	"github.com/ei4869/SharePipe-OSS/internal/security"
	"github.com/ei4869/SharePipe-OSS/internal/storage"
)

type uploadSession struct {
	ID               string
	ShareID          string
	EntryID          string
	Backend          string
	StorageProfileID string
	ExpectedSize     int64
	ChunkSize        int64
	ExpectedParts    int
	ReceivedBytes    int64
	S3UploadID       sql.NullString
	Status           string
	ExpiresAt        time.Time
	EntryPath        string
	MIMEType         string
	StorageKey       string
	ShareStatus      string
	TaskStatus       string
	CompletionOwner  sql.NullString
}

type UploadProgress struct {
	SessionID     string            `json:"sessionId"`
	Status        string            `json:"status"`
	ReceivedBytes int64             `json:"receivedBytes"`
	ExpectedSize  int64             `json:"expectedSize"`
	UploadedParts int               `json:"uploadedParts"`
	ExpectedParts int               `json:"expectedParts"`
	Parts         []core.UploadPart `json:"parts"`
}

func (s *Service) PutLocalPart(ctx context.Context, sessionID, token string, partNumber int, body io.Reader) (UploadProgress, error) {
	session, err := s.authenticateUpload(ctx, sessionID, token)
	if err != nil {
		return UploadProgress{}, err
	}
	if session.Backend != "local" {
		return UploadProgress{}, fmt.Errorf("%w: upload uses S3", core.ErrConflict)
	}
	expected, err := expectedPartSize(session, partNumber)
	if err != nil {
		return UploadProgress{}, err
	}
	partDirectory := filepath.Join(s.stagingRoot, session.ID)
	if err := os.MkdirAll(partDirectory, 0o750); err != nil {
		return UploadProgress{}, fmt.Errorf("create part directory: %w", err)
	}
	finalPath := filepath.Join(partDirectory, fmt.Sprintf("%06d.part", partNumber))
	temp, err := os.CreateTemp(partDirectory, ".part-*")
	if err != nil {
		return UploadProgress{}, fmt.Errorf("create part file: %w", err)
	}
	tempPath := temp.Name()
	keep := false
	defer func() {
		temp.Close()
		if !keep {
			_ = os.Remove(tempPath)
		}
	}()
	hash := sha256.New()
	written, err := copyExact(ctx, io.MultiWriter(temp, hash), body, expected)
	if err != nil {
		return UploadProgress{}, err
	}
	if written != expected {
		return UploadProgress{}, fmt.Errorf("%w: part size mismatch, expected %d and received %d", core.ErrValidation, expected, written)
	}
	if err := temp.Sync(); err != nil {
		return UploadProgress{}, err
	}
	if err := temp.Close(); err != nil {
		return UploadProgress{}, err
	}
	checksum := hex.EncodeToString(hash.Sum(nil))
	var existingSize int64
	var existingChecksum sql.NullString
	queryErr := s.db.QueryRowContext(ctx, `SELECT size, checksum FROM upload_parts
		WHERE session_id=? AND part_number=?`, session.ID, partNumber).Scan(&existingSize, &existingChecksum)
	switch {
	case queryErr == nil:
		if existingSize != expected || !existingChecksum.Valid || existingChecksum.String != checksum {
			return UploadProgress{}, core.ErrConflict
		}
		return s.UploadProgress(ctx, session.ID, token)
	case !errors.Is(queryErr, sql.ErrNoRows):
		return UploadProgress{}, queryErr
	}
	if err := os.Remove(finalPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return UploadProgress{}, err
	}
	if err := os.Rename(tempPath, finalPath); err != nil {
		return UploadProgress{}, fmt.Errorf("commit part file: %w", err)
	}
	keep = true
	now := s.now().Unix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return UploadProgress{}, err
	}
	defer tx.Rollback()
	guard, err := tx.ExecContext(ctx, `UPDATE upload_tasks SET updated_at=CASE WHEN status='uploading' THEN ? ELSE updated_at END
		WHERE share_id=? AND (status='uploading' OR (status='completing' AND completion_session_id!=?))
		  AND EXISTS(SELECT 1 FROM upload_sessions WHERE id=? AND share_id=? AND status='open')`,
		now, session.ShareID, session.ID, session.ID, session.ShareID)
	if err != nil {
		return UploadProgress{}, err
	}
	if changed, _ := guard.RowsAffected(); changed != 1 {
		return UploadProgress{}, core.ErrConflict
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO upload_parts(
		session_id, part_number, size, checksum, created_at
	) VALUES(?, ?, ?, ?, ?)`, session.ID, partNumber, expected, checksum, now); err != nil {
		return UploadProgress{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE upload_sessions SET
		received_bytes=(SELECT COALESCE(SUM(size),0) FROM upload_parts WHERE session_id=?),
		updated_at=? WHERE id=?`, session.ID, now, session.ID); err != nil {
		return UploadProgress{}, err
	}
	if err := tx.Commit(); err != nil {
		return UploadProgress{}, err
	}
	return s.UploadProgress(ctx, session.ID, token)
}

func (s *Service) PresignS3Part(ctx context.Context, sessionID, token string, partNumber int) (string, error) {
	session, err := s.authenticateUpload(ctx, sessionID, token)
	if err != nil {
		return "", err
	}
	if session.Backend != "s3" || !session.S3UploadID.Valid {
		return "", fmt.Errorf("%w: multipart upload is not initialized", core.ErrConflict)
	}
	if _, err := expectedPartSize(session, partNumber); err != nil {
		return "", err
	}
	store, err := s.storeFor(session.StorageProfileID, session.Backend)
	if err != nil {
		return "", err
	}
	multipart := store.(storage.MultipartStore)
	return multipart.PresignUploadPart(ctx, session.StorageKey, session.S3UploadID.String, partNumber, 15*time.Minute)
}

type CompleteUploadInput struct {
	Parts []core.UploadPart `json:"parts"`
}

func (s *Service) claimUploadCompletion(ctx context.Context, session uploadSession) error {
	now := s.now().Unix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE upload_tasks
		SET status='completing',completion_session_id=?,updated_at=?
		WHERE share_id=? AND status='uploading'
		  AND EXISTS(SELECT 1 FROM upload_sessions WHERE id=? AND share_id=? AND status='open')
		  AND EXISTS(SELECT 1 FROM shares WHERE id=? AND status='uploading')`,
		session.ID, now, session.ShareID, session.ID, session.ShareID, session.ShareID)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return core.ErrConflict
	}
	result, err = tx.ExecContext(ctx, `UPDATE upload_sessions SET status='assembling',updated_at=?
		WHERE id=? AND share_id=? AND status='open'
		  AND EXISTS(SELECT 1 FROM upload_tasks WHERE share_id=? AND status='completing' AND completion_session_id=?)`,
		now, session.ID, session.ShareID, session.ShareID, session.ID)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return core.ErrConflict
	}
	return tx.Commit()
}

func (s *Service) releaseUploadCompletion(ctx context.Context, session uploadSession) error {
	now := s.now().Unix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE upload_sessions SET status='open',updated_at=?
		WHERE id=? AND status='assembling'
		  AND EXISTS(SELECT 1 FROM upload_tasks WHERE share_id=? AND status='completing' AND completion_session_id=?)`,
		now, session.ID, session.ShareID, session.ID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE upload_tasks SET status=CASE
			WHEN EXISTS(SELECT 1 FROM shares WHERE id=? AND status='uploading') THEN 'uploading'
			WHEN EXISTS(SELECT 1 FROM shares WHERE id=? AND status='expired') THEN 'expired'
			ELSE 'cancel_requested' END,
		completion_session_id=NULL,updated_at=?
		WHERE share_id=? AND status='completing' AND completion_session_id=?`,
		session.ShareID, session.ShareID, now, session.ShareID, session.ID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) acquireUploadCompletion(ctx context.Context, session uploadSession) error {
	for attempt := 0; attempt < 150; attempt++ {
		err := s.claimUploadCompletion(ctx, session)
		if err == nil {
			return nil
		}
		if !errors.Is(err, core.ErrConflict) {
			return err
		}
		var taskStatus, sessionStatus string
		var owner sql.NullString
		queryErr := s.db.QueryRowContext(ctx, `SELECT t.status,t.completion_session_id,u.status
			FROM upload_tasks t JOIN upload_sessions u ON u.share_id=t.share_id
			WHERE t.share_id=? AND u.id=?`, session.ShareID, session.ID).Scan(&taskStatus, &owner, &sessionStatus)
		if queryErr != nil {
			return queryErr
		}
		if taskStatus == "uploading" && sessionStatus == "open" {
			continue
		}
		if taskStatus != "completing" || !owner.Valid || owner.String == session.ID || sessionStatus != "open" {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return core.ErrConflict
}

func (s *Service) CompleteUpload(ctx context.Context, sessionID, token string, input CompleteUploadInput) (UploadProgress, error) {
	session, err := s.authenticateUpload(ctx, sessionID, token)
	if err != nil {
		return UploadProgress{}, err
	}
	if err := s.acquireUploadCompletion(ctx, session); err != nil {
		if errors.Is(err, core.ErrConflict) {
			return UploadProgress{}, core.ErrConflict
		}
		return UploadProgress{}, err
	}
	completionOwned := true
	defer func() {
		if completionOwned {
			releaseContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			_ = s.releaseUploadCompletion(releaseContext, session)
		}
	}()

	var info storage.ObjectInfo
	detectedMIME := session.MIMEType
	var checksum string
	if session.Backend == "local" {
		info, detectedMIME, checksum, err = s.completeLocalUpload(ctx, session)
	} else {
		parts := make([]storage.MultipartPart, len(input.Parts))
		for index, part := range input.Parts {
			parts[index] = storage.MultipartPart{PartNumber: part.PartNumber, ETag: part.ETag}
		}
		info, err = s.completeS3Upload(ctx, session, parts)
	}
	if err != nil {
		_, _ = s.db.ExecContext(ctx, "UPDATE upload_sessions SET status='open', updated_at=? WHERE id=?",
			s.now().Unix(), session.ID)
		return UploadProgress{}, err
	}
	if info.Size != session.ExpectedSize {
		return UploadProgress{}, fmt.Errorf("%w: completed object size mismatch", core.ErrUploadIncomplete)
	}
	if s.cfg.Scanner.Enabled {
		scanResult, scanErr := s.scanCompletedObject(ctx, session)
		if scanErr != nil && !s.cfg.Scanner.FailClosed {
			s.audit(ctx, session.ShareID, "scan_upload", "error_ignored", "", "", scanErr.Error())
		} else if scanErr != nil || scanResult.Verdict == scanner.VerdictInfected {
			reason := "malware_detected"
			if scanErr != nil {
				reason = "scanner_error"
			}
			if err := s.rejectScannedUpload(ctx, session, reason); err != nil {
				return UploadProgress{}, err
			}
			detail := scanResult.Detail
			if scanErr != nil {
				detail = scanErr.Error()
			}
			s.audit(ctx, session.ShareID, "scan_upload", reason, "", "", detail)
			return UploadProgress{}, fmt.Errorf("%w: %s", core.ErrUnsafeUpload, reason)
		}
	}

	now := s.now().Unix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return UploadProgress{}, err
	}
	defer tx.Rollback()
	var checksumValue any
	if checksum != "" {
		checksumValue = checksum
	}
	if _, err := tx.ExecContext(ctx, `UPDATE entries SET status='ready', sha256=?, mime_type=?
		WHERE id=? AND status='pending'`, checksumValue, detectedMIME, session.EntryID); err != nil {
		return UploadProgress{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE upload_sessions SET status='completed',
		received_bytes=expected_size, updated_at=? WHERE id=?`, now, session.ID); err != nil {
		return UploadProgress{}, err
	}
	var pending int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM entries
		WHERE share_id=? AND kind='file' AND status='pending'`, session.ShareID).Scan(&pending); err != nil {
		return UploadProgress{}, err
	}
	if pending == 0 {
		if _, err := tx.ExecContext(ctx, `UPDATE shares SET status='active', updated_at=?
			WHERE id=? AND status='uploading'`, now, session.ShareID); err != nil {
			return UploadProgress{}, err
		}
	}
	var shareStatus string
	if err := tx.QueryRowContext(ctx, "SELECT status FROM shares WHERE id=?", session.ShareID).Scan(&shareStatus); err != nil {
		return UploadProgress{}, err
	}
	taskStatus := "cancel_requested"
	if shareStatus == "active" {
		taskStatus = "completed"
	} else if shareStatus == "uploading" {
		taskStatus = "uploading"
	} else if shareStatus == "expired" {
		taskStatus = "expired"
	}
	result, err := tx.ExecContext(ctx, `UPDATE upload_tasks SET status=?,completion_session_id=NULL,updated_at=?
		WHERE share_id=? AND status='completing' AND completion_session_id=?`, taskStatus, now, session.ShareID, session.ID)
	if err != nil {
		return UploadProgress{}, err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return UploadProgress{}, core.ErrConflict
	}
	if err := tx.Commit(); err != nil {
		return UploadProgress{}, err
	}
	completionOwned = false
	if pending == 0 {
		if _, err := s.ActivatePairCode(ctx, session.ShareID); err != nil && !errors.Is(err, core.ErrConflict) {
			return UploadProgress{}, err
		}
	}
	if session.Backend == "local" {
		_ = os.RemoveAll(filepath.Join(s.stagingRoot, session.ID))
	}
	return UploadProgress{
		SessionID: session.ID, Status: "completed", ReceivedBytes: session.ExpectedSize,
		ExpectedSize: session.ExpectedSize, UploadedParts: session.ExpectedParts, ExpectedParts: session.ExpectedParts,
	}, nil
}

func (s *Service) scanCompletedObject(ctx context.Context, session uploadSession) (scanner.Result, error) {
	store, err := s.storeFor(session.StorageProfileID, session.Backend)
	if err != nil {
		return scanner.Result{}, err
	}
	object, err := store.Open(ctx, session.StorageKey, nil)
	if err != nil {
		return scanner.Result{}, err
	}
	defer object.Body.Close()
	return s.scanner.Scan(ctx, scanner.Metadata{
		Path: session.EntryPath, MIMEType: session.MIMEType, Size: session.ExpectedSize,
	}, object.Body)
}

func (s *Service) rejectScannedUpload(ctx context.Context, session uploadSession, reason string) error {
	if store, err := s.storeFor(session.StorageProfileID, session.Backend); err == nil {
		_ = store.Delete(ctx, session.StorageKey)
	}
	now := s.now().Unix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "UPDATE entries SET status='quarantined' WHERE id=?", session.EntryID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE upload_sessions SET status='completed', updated_at=? WHERE id=?`, now, session.ID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE shares SET status='revoked', revoked_at=?, updated_at=? WHERE id=?`, now, now, session.ShareID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if session.Backend == "local" {
		_ = os.RemoveAll(filepath.Join(s.stagingRoot, session.ID))
	}
	return nil
}

func (s *Service) completeLocalUpload(ctx context.Context, session uploadSession) (storage.ObjectInfo, string, string, error) {
	store, err := s.storeFor(session.StorageProfileID, session.Backend)
	if err != nil {
		return storage.ObjectInfo{}, "", "", err
	}
	if session.ExpectedParts == 0 {
		info, err := store.Put(ctx, session.StorageKey, bytes.NewReader(nil), 0)
		return info, "application/octet-stream", info.ETag, err
	}
	var partCount int
	var total int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(size),0)
		FROM upload_parts WHERE session_id=?`, session.ID).Scan(&partCount, &total); err != nil {
		return storage.ObjectInfo{}, "", "", err
	}
	if partCount != session.ExpectedParts || total != session.ExpectedSize {
		return storage.ObjectInfo{}, "", "", core.ErrUploadIncomplete
	}
	files := make([]*os.File, 0, session.ExpectedParts)
	readers := make([]io.Reader, 0, session.ExpectedParts)
	defer func() {
		for _, file := range files {
			file.Close()
		}
	}()
	var sniff []byte
	for part := 1; part <= session.ExpectedParts; part++ {
		file, err := os.Open(filepath.Join(s.stagingRoot, session.ID, fmt.Sprintf("%06d.part", part)))
		if err != nil {
			return storage.ObjectInfo{}, "", "", core.ErrUploadIncomplete
		}
		files = append(files, file)
		readers = append(readers, file)
		if part == 1 {
			buffer := make([]byte, 512)
			count, _ := file.Read(buffer)
			sniff = append([]byte(nil), buffer[:count]...)
			if _, err := file.Seek(0, io.SeekStart); err != nil {
				return storage.ObjectInfo{}, "", "", err
			}
		}
	}
	info, err := store.Put(ctx, session.StorageKey, io.MultiReader(readers...), session.ExpectedSize)
	if err != nil {
		if existing, statErr := store.Stat(ctx, session.StorageKey); statErr == nil && existing.Size == session.ExpectedSize {
			info = existing
		} else {
			return storage.ObjectInfo{}, "", "", err
		}
	}
	detected := http.DetectContentType(sniff)
	if detected == "application/octet-stream" && session.MIMEType != "" {
		detected = session.MIMEType
	}
	return info, detected, info.ETag, nil
}

func (s *Service) completeS3Upload(ctx context.Context, session uploadSession, parts []storage.MultipartPart) (storage.ObjectInfo, error) {
	store, err := s.storeFor(session.StorageProfileID, session.Backend)
	if err != nil {
		return storage.ObjectInfo{}, err
	}
	if session.ExpectedParts == 0 {
		return store.Put(ctx, session.StorageKey, bytes.NewReader(nil), 0)
	}
	if !session.S3UploadID.Valid || len(parts) != session.ExpectedParts {
		return storage.ObjectInfo{}, core.ErrUploadIncomplete
	}
	sort.Slice(parts, func(i, j int) bool { return parts[i].PartNumber < parts[j].PartNumber })
	for index, part := range parts {
		if part.PartNumber != index+1 || part.ETag == "" {
			return storage.ObjectInfo{}, fmt.Errorf("%w: invalid S3 part list", core.ErrValidation)
		}
	}
	info, err := store.(storage.MultipartStore).CompleteMultipart(ctx, session.StorageKey, session.S3UploadID.String, parts)
	if err == nil {
		return info, nil
	}
	existing, statErr := store.Stat(ctx, session.StorageKey)
	if statErr == nil && existing.Size == session.ExpectedSize {
		return existing, nil
	}
	return storage.ObjectInfo{}, err
}

func (s *Service) UploadProgress(ctx context.Context, sessionID, token string) (UploadProgress, error) {
	session, err := s.authenticateUpload(ctx, sessionID, token)
	if err != nil {
		if errors.Is(err, core.ErrConflict) {
			var progress UploadProgress
			var count int
			queryErr := s.db.QueryRowContext(ctx, `SELECT u.id, u.status, u.received_bytes, u.expected_size,
				expected_parts, (SELECT COUNT(*) FROM upload_parts WHERE session_id=u.id)
				FROM upload_sessions u JOIN shares sh ON sh.id=u.share_id
				WHERE u.id=? AND (u.token_hash=? OR sh.manage_token_hash=?)`,
				sessionID, security.HashToken(token), security.HashToken(token)).Scan(
				&progress.SessionID, &progress.Status, &progress.ReceivedBytes, &progress.ExpectedSize,
				&progress.ExpectedParts, &count)
			progress.UploadedParts = count
			if queryErr == nil {
				progress.Parts, queryErr = s.uploadParts(ctx, sessionID)
			}
			return progress, queryErr
		}
		return UploadProgress{}, err
	}
	var uploaded int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM upload_parts WHERE session_id=?", session.ID).Scan(&uploaded); err != nil {
		return UploadProgress{}, err
	}
	parts, err := s.uploadParts(ctx, session.ID)
	if err != nil {
		return UploadProgress{}, err
	}
	return UploadProgress{
		SessionID: session.ID, Status: session.Status, ReceivedBytes: session.ReceivedBytes,
		ExpectedSize: session.ExpectedSize, UploadedParts: uploaded, ExpectedParts: session.ExpectedParts, Parts: parts,
	}, nil
}

func (s *Service) AcknowledgeS3Part(ctx context.Context, sessionID, token string, part core.UploadPart) (UploadProgress, error) {
	session, err := s.authenticateUpload(ctx, sessionID, token)
	if err != nil {
		return UploadProgress{}, err
	}
	if session.Backend != "s3" || !session.S3UploadID.Valid {
		return UploadProgress{}, fmt.Errorf("%w: multipart upload is not initialized", core.ErrConflict)
	}
	expected, err := expectedPartSize(session, part.PartNumber)
	if err != nil {
		return UploadProgress{}, err
	}
	part.Size = expected
	if part.ETag == "" || len(part.ETag) > 512 || strings.ContainsAny(part.ETag, "\r\n") {
		return UploadProgress{}, fmt.Errorf("%w: invalid S3 part acknowledgement", core.ErrValidation)
	}
	digest, err := hex.DecodeString(part.SHA256)
	if err != nil || len(digest) != sha256.Size {
		return UploadProgress{}, fmt.Errorf("%w: invalid part SHA-256", core.ErrValidation)
	}
	now := s.now().Unix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return UploadProgress{}, err
	}
	defer tx.Rollback()
	guard, err := tx.ExecContext(ctx, `UPDATE upload_tasks SET updated_at=CASE WHEN status='uploading' THEN ? ELSE updated_at END
		WHERE share_id=? AND (status='uploading' OR (status='completing' AND completion_session_id!=?))
		  AND EXISTS(SELECT 1 FROM upload_sessions WHERE id=? AND share_id=? AND status='open')`,
		now, session.ShareID, session.ID, session.ID, session.ShareID)
	if err != nil {
		return UploadProgress{}, err
	}
	if changed, _ := guard.RowsAffected(); changed != 1 {
		return UploadProgress{}, core.ErrConflict
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO upload_parts(session_id,part_number,size,checksum,etag,created_at)
		VALUES(?,?,?,?,?,?) ON CONFLICT(session_id,part_number) DO UPDATE SET
		size=excluded.size,checksum=excluded.checksum,etag=excluded.etag,created_at=excluded.created_at`,
		session.ID, part.PartNumber, part.Size, part.SHA256, part.ETag, now); err != nil {
		return UploadProgress{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE upload_sessions SET
		received_bytes=(SELECT COALESCE(SUM(size),0) FROM upload_parts WHERE session_id=?),updated_at=? WHERE id=?`,
		session.ID, now, session.ID); err != nil {
		return UploadProgress{}, err
	}
	if err := tx.Commit(); err != nil {
		return UploadProgress{}, err
	}
	return s.UploadProgress(ctx, session.ID, token)
}

func (s *Service) uploadParts(ctx context.Context, sessionID string) ([]core.UploadPart, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT part_number,size,COALESCE(etag,''),COALESCE(checksum,'')
		FROM upload_parts WHERE session_id=? ORDER BY part_number`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	parts := make([]core.UploadPart, 0)
	for rows.Next() {
		var part core.UploadPart
		if err := rows.Scan(&part.PartNumber, &part.Size, &part.ETag, &part.SHA256); err != nil {
			return nil, err
		}
		parts = append(parts, part)
	}
	return parts, rows.Err()
}

func (s *Service) authenticateUpload(ctx context.Context, sessionID, token string) (uploadSession, error) {
	var session uploadSession
	var expires int64
	err := s.db.QueryRowContext(ctx, `SELECT u.id, u.share_id, u.entry_id, u.backend, u.storage_profile_id,
		u.expected_size, u.chunk_size, u.expected_parts, u.received_bytes, u.s3_upload_id,
		u.status, u.expires_at, e.path, e.mime_type, e.storage_key, sh.status, t.status,t.completion_session_id
		FROM upload_sessions u
		JOIN entries e ON e.id=u.entry_id
		JOIN shares sh ON sh.id=u.share_id
		JOIN upload_tasks t ON t.share_id=sh.id
		WHERE u.id=? AND (u.token_hash=? OR sh.manage_token_hash=?)
		  AND sh.status IN ('uploading','active')`, sessionID, security.HashToken(token), security.HashToken(token)).Scan(
		&session.ID, &session.ShareID, &session.EntryID, &session.Backend, &session.StorageProfileID,
		&session.ExpectedSize, &session.ChunkSize, &session.ExpectedParts, &session.ReceivedBytes,
		&session.S3UploadID, &session.Status, &expires, &session.EntryPath, &session.MIMEType,
		&session.StorageKey, &session.ShareStatus, &session.TaskStatus, &session.CompletionOwner,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return uploadSession{}, core.ErrUnauthorized
	}
	if err != nil {
		return uploadSession{}, err
	}
	session.ExpiresAt = time.Unix(expires, 0).UTC()
	if !session.ExpiresAt.After(s.now()) {
		_, _ = s.db.ExecContext(ctx, "UPDATE upload_sessions SET status='expired', updated_at=? WHERE id=? AND status='open'",
			s.now().Unix(), session.ID)
		return uploadSession{}, core.ErrExpired
	}
	if session.Status != "open" {
		return uploadSession{}, core.ErrConflict
	}
	if session.ShareStatus != "uploading" {
		return uploadSession{}, core.ErrConflict
	}
	if session.TaskStatus != "uploading" && !(session.TaskStatus == "completing" && session.CompletionOwner.Valid && session.CompletionOwner.String != session.ID) {
		return uploadSession{}, core.ErrConflict
	}
	return session, nil
}

func expectedPartSize(session uploadSession, partNumber int) (int64, error) {
	if partNumber < 1 || partNumber > session.ExpectedParts {
		return 0, fmt.Errorf("%w: invalid part number", core.ErrValidation)
	}
	if partNumber < session.ExpectedParts {
		return session.ChunkSize, nil
	}
	return session.ExpectedSize - int64(session.ExpectedParts-1)*session.ChunkSize, nil
}

func copyExact(ctx context.Context, destination io.Writer, source io.Reader, expected int64) (int64, error) {
	reader := io.LimitReader(source, expected+1)
	buffer := make([]byte, 128*1024)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		count, readErr := reader.Read(buffer)
		if count > 0 {
			written, writeErr := destination.Write(buffer[:count])
			total += int64(written)
			if writeErr != nil {
				return total, writeErr
			}
			if written != count {
				return total, io.ErrShortWrite
			}
			if total > expected {
				return total, fmt.Errorf("%w: part is larger than expected", core.ErrValidation)
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return total, nil
			}
			return total, readErr
		}
	}
}
