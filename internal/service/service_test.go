package service

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ei4869/SharePipe-OSS/internal/config"
	"github.com/ei4869/SharePipe-OSS/internal/core"
	"github.com/ei4869/SharePipe-OSS/internal/database"
	"github.com/ei4869/SharePipe-OSS/internal/scanner"
	"github.com/ei4869/SharePipe-OSS/internal/security"
	"github.com/ei4869/SharePipe-OSS/internal/storage"
)

func newTestService(t *testing.T) (*Service, config.Config) {
	t.Helper()
	root := t.TempDir()
	cfg := config.Defaults().ResolvePaths(root)
	cfg.Backup.Hour = 0
	db, err := database.Open(context.Background(), cfg.Database.Path, 1000)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	local, err := storage.NewLocal(cfg.Storage.Local.Root)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := security.NewSigner([]byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatal(err)
	}
	adminHash, err := security.HashPassword("administrator password")
	if err != nil {
		t.Fatal(err)
	}
	svc, err := New(Options{
		Database: db, Config: cfg, Stores: map[string]storage.Store{"local": local},
		Signer: signer, AdminPasswordHash: adminHash,
		StorageMasterKey: bytes.Repeat([]byte{0x31}, 32),
		StagingRoot:      filepath.Join(root, "workdir", "intermediate", "uploads"),
		AuditSalt:        []byte("01234567890123456789012345678901"),
	})
	if err != nil {
		t.Fatal(err)
	}
	return svc, cfg
}

type infectedScanner struct{}

func (infectedScanner) Scan(context.Context, scanner.Metadata, io.Reader) (scanner.Result, error) {
	return scanner.Result{Verdict: scanner.VerdictInfected, Detail: "test signature"}, nil
}

func TestScannerQuarantinesAndRevokesShare(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t)
	svc.cfg.Scanner.Enabled = true
	svc.scanner = infectedScanner{}

	maxUses := 1
	invite, err := svc.CreateInvite(ctx, "scanner", &maxUses, nil)
	if err != nil {
		t.Fatal(err)
	}
	creation, err := svc.CreateShare(ctx, CreateShareInput{
		InviteCode: invite.Code, Name: "scanner fixture", ExpiresIn: "24h",
		Entries: []core.ManifestEntry{{Path: "payload.bin", Kind: "file", Size: 4}},
	})
	if err != nil {
		t.Fatal(err)
	}
	upload := creation.Uploads[0]
	if _, err := svc.PutLocalPart(ctx, upload.SessionID, upload.Token, 1, bytes.NewBufferString("test")); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CompleteUpload(ctx, upload.SessionID, upload.Token, CompleteUploadInput{}); !errors.Is(err, core.ErrUnsafeUpload) {
		t.Fatalf("expected unsafe upload rejection, received %v", err)
	}
	managed, err := svc.GetManagedShare(ctx, creation.ManagementToken)
	if err != nil {
		t.Fatal(err)
	}
	if managed.Share.Status != "revoked" || managed.Entries[0].Status != "quarantined" {
		t.Fatalf("unexpected quarantined share: %#v", managed)
	}
}

func TestAdministratorCanRevokeShareByID(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t)
	maxUses := 1
	invite, err := svc.CreateInvite(ctx, "admin", &maxUses, nil)
	if err != nil {
		t.Fatal(err)
	}
	creation, err := svc.CreateShare(ctx, CreateShareInput{
		InviteCode: invite.Code, Name: "admin fixture", ExpiresIn: "24h",
		Entries: []core.ManifestEntry{{Path: "file.txt", Kind: "file", Size: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RevokeShareByID(ctx, creation.Share.ID); err != nil {
		t.Fatal(err)
	}
	managed, err := svc.GetManagedShare(ctx, creation.ManagementToken)
	if err != nil {
		t.Fatal(err)
	}
	if managed.Share.Status != "revoked" {
		t.Fatalf("expected revoked status, received %s", managed.Share.Status)
	}
}

func TestUnlimitedDefaultPersistsAndDoesNotRetroactivelyChange(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t)
	unlimited := 0
	if _, err := svc.UpdateSettings(ctx, UpdateRuntimeSettings{DefaultDownloadLimit: &unlimited}); err != nil {
		t.Fatal(err)
	}
	creation, err := svc.CreateShare(ctx, CreateShareInput{
		LocalSystem: true, Name: "unlimited", ExpiresIn: "24h",
		Entries: []core.ManifestEntry{{Path: "empty.txt", Kind: "file", Size: 0, MIMEType: "text/plain"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	upload := creation.Uploads[0]
	if _, err := svc.CompleteUpload(ctx, upload.SessionID, upload.Token, CompleteUploadInput{}); err != nil {
		t.Fatal(err)
	}
	public, err := svc.GetPublicShare(ctx, creation.PublicToken, true)
	if err != nil {
		t.Fatal(err)
	}
	if public.Share.DownloadLimit != nil {
		t.Fatalf("default zero should persist as unlimited, got %v", *public.Share.DownloadLimit)
	}
	for attempt := 0; attempt < 12; attempt++ {
		if _, err := svc.CreateDownloadGrant(ctx, creation.PublicToken, true, public.Entries[0].ID, "file"); err != nil {
			t.Fatalf("unlimited grant %d failed: %v", attempt+1, err)
		}
	}
	positive := 3
	if _, err := svc.UpdateSettings(ctx, UpdateRuntimeSettings{DefaultDownloadLimit: &positive}); err != nil {
		t.Fatal(err)
	}
	second, err := svc.CreateShare(ctx, CreateShareInput{
		LocalSystem: true, Name: "limited", ExpiresIn: "24h",
		Entries: []core.ManifestEntry{{Path: "second.txt", Kind: "file", Size: 0, MIMEType: "text/plain"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.Share.DownloadLimit == nil || *second.Share.DownloadLimit != positive {
		t.Fatalf("new share limit = %v", second.Share.DownloadLimit)
	}
	managed, err := svc.GetManagedShare(ctx, creation.ManagementToken)
	if err != nil {
		t.Fatal(err)
	}
	if managed.Share.DownloadLimit != nil || managed.Share.DownloadCount != 12 {
		t.Fatalf("existing unlimited share changed: %#v", managed.Share)
	}
}

func TestSharePasswordVersionChangesOnlyWhenPasswordIsUpdated(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t)
	creation, err := svc.CreateShare(ctx, CreateShareInput{
		LocalSystem: true, Name: "password version", ExpiresIn: "24h", Password: "initial-password",
		Entries: []core.ManifestEntry{{Path: "empty.txt", Kind: "file", Size: 0}},
	})
	if err != nil {
		t.Fatal(err)
	}
	before, err := svc.SharePasswordVersion(ctx, creation.Share.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateManagedShare(ctx, creation.ManagementToken, UpdateShareInput{}); err != nil {
		t.Fatal(err)
	}
	afterMetadata, err := svc.SharePasswordVersion(ctx, creation.Share.ID)
	if err != nil {
		t.Fatal(err)
	}
	if afterMetadata != before {
		t.Fatalf("password version changed without password update: %d -> %d", before, afterMetadata)
	}
	replacement := "replacement-password"
	if _, err := svc.UpdateManagedShare(ctx, creation.ManagementToken, UpdateShareInput{Password: &replacement}); err != nil {
		t.Fatal(err)
	}
	afterPassword, err := svc.SharePasswordVersion(ctx, creation.Share.ID)
	if err != nil {
		t.Fatal(err)
	}
	if afterPassword != before+1 {
		t.Fatalf("password version = %d, want %d", afterPassword, before+1)
	}
}

func TestUploadTaskPauseResumeAndCancelAreServerPersisted(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t)
	creation, err := svc.CreateShare(ctx, CreateShareInput{
		LocalSystem: true, Name: "task controls", ExpiresIn: "24h",
		Entries: []core.ManifestEntry{{Path: "task.txt", Kind: "file", Size: 4, MIMEType: "text/plain"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	task, err := svc.PauseUploadTask(ctx, creation.Share.ID, creation.ManagementToken)
	if err != nil || task.Status != "paused" {
		t.Fatalf("pause task = %#v, %v", task, err)
	}
	if _, err := svc.PauseUploadTask(ctx, creation.Share.ID, creation.ManagementToken); err != nil {
		t.Fatalf("pause should be idempotent: %v", err)
	}
	recovered, err := svc.RecoverUpload(ctx, creation.ManagementToken)
	if err != nil || recovered.UploadTaskStatus != "paused" {
		t.Fatalf("recover paused task = %#v, %v", recovered, err)
	}
	upload := creation.Uploads[0]
	if _, err := svc.PutLocalPart(ctx, upload.SessionID, upload.Token, 1, bytes.NewBufferString("test")); !errors.Is(err, core.ErrConflict) {
		t.Fatalf("paused task accepted a part: %v", err)
	}
	task, err = svc.ResumeUploadTask(ctx, creation.Share.ID, creation.ManagementToken)
	if err != nil || task.Status != "uploading" {
		t.Fatalf("resume task = %#v, %v", task, err)
	}
	if _, err := svc.PutLocalPart(ctx, upload.SessionID, upload.Token, 1, bytes.NewBufferString("test")); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PauseUploadTask(ctx, creation.Share.ID, creation.ManagementToken); err != nil {
		t.Fatal(err)
	}
	task, err = svc.CancelUploadTask(ctx, creation.Share.ID, creation.ManagementToken)
	if err != nil || task.Status != "aborted" {
		t.Fatalf("cancel task = %#v, %v", task, err)
	}
	if _, err := svc.CancelUploadTask(ctx, creation.Share.ID, creation.ManagementToken); err != nil {
		t.Fatalf("cancel should be idempotent: %v", err)
	}
	if _, err := svc.RecoverUpload(ctx, creation.ManagementToken); !errors.Is(err, core.ErrExpired) {
		t.Fatalf("cancelled recovery remained usable: %v", err)
	}
}

func TestUploadCompletionOwnershipExcludesPauseCancelAndParts(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t)
	creation, err := svc.CreateShare(ctx, CreateShareInput{
		LocalSystem: true, Name: "atomic completion", ExpiresIn: "24h",
		Entries: []core.ManifestEntry{
			{Path: "a.txt", Kind: "file", Size: 1, MIMEType: "text/plain"},
			{Path: "b.txt", Kind: "file", Size: 1, MIMEType: "text/plain"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := svc.authenticateUpload(ctx, creation.Uploads[0].SessionID, creation.Uploads[0].Token)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.claimUploadCompletion(ctx, first); err != nil {
		t.Fatal(err)
	}
	var status string
	var owner sql.NullString
	if err := svc.db.QueryRowContext(ctx, "SELECT status,completion_session_id FROM upload_tasks WHERE share_id=?", creation.Share.ID).Scan(&status, &owner); err != nil {
		t.Fatal(err)
	}
	if status != "completing" || !owner.Valid || owner.String != first.ID {
		t.Fatalf("completion owner = %q/%#v", status, owner)
	}
	if _, err := svc.PauseUploadTask(ctx, creation.Share.ID, creation.ManagementToken); !errors.Is(err, core.ErrConflict) {
		t.Fatalf("pause acquired a completing task: %v", err)
	}
	if _, err := svc.CancelUploadTask(ctx, creation.Share.ID, creation.ManagementToken); !errors.Is(err, core.ErrConflict) {
		t.Fatalf("cancel acquired a completing task: %v", err)
	}
	if _, err := svc.PutLocalPart(ctx, creation.Uploads[0].SessionID, creation.Uploads[0].Token, 1, bytes.NewBufferString("a")); !errors.Is(err, core.ErrConflict) {
		t.Fatalf("completion owner accepted another part: %v", err)
	}
	if _, err := svc.PutLocalPart(ctx, creation.Uploads[1].SessionID, creation.Uploads[1].Token, 1, bytes.NewBufferString("b")); err != nil {
		t.Fatalf("independent session was unnecessarily blocked: %v", err)
	}
	if err := svc.releaseUploadCompletion(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := svc.db.QueryRowContext(ctx, "SELECT status,completion_session_id FROM upload_tasks WHERE share_id=?", creation.Share.ID).Scan(&status, &owner); err != nil {
		t.Fatal(err)
	}
	if status != "uploading" || owner.Valid {
		t.Fatalf("released completion owner = %q/%#v", status, owner)
	}
}

func TestCancelledUploadCleanupRetriesBeforeAborted(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t)
	creation, err := svc.CreateShare(ctx, CreateShareInput{
		LocalSystem: true, Name: "cleanup retry", ExpiresIn: "24h",
		Entries: []core.ManifestEntry{{Path: "retry.txt", Kind: "file", Size: 1, MIMEType: "text/plain"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	upload := creation.Uploads[0]
	var originalBackend, originalProfile string
	if err := svc.db.QueryRowContext(ctx, "SELECT backend,storage_profile_id FROM upload_sessions WHERE id=?", upload.SessionID).Scan(&originalBackend, &originalProfile); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.db.ExecContext(ctx, "UPDATE upload_sessions SET backend='s3',storage_profile_id='missing-profile',s3_upload_id='missing-upload' WHERE id=?", upload.SessionID); err != nil {
		t.Fatal(err)
	}
	task, err := svc.CancelUploadTask(ctx, creation.Share.ID, creation.ManagementToken)
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != "cancel_requested" {
		t.Fatalf("task reached terminal state before cleanup: %s", task.Status)
	}
	var shareStatus, cleanupError string
	var attempts int
	if err := svc.db.QueryRowContext(ctx, "SELECT status,COALESCE(cleanup_error,''),cleanup_attempts FROM shares WHERE id=?", creation.Share.ID).Scan(&shareStatus, &cleanupError, &attempts); err != nil {
		t.Fatal(err)
	}
	if shareStatus != "cleanup_pending" || cleanupError == "" || attempts != 1 {
		t.Fatalf("cleanup failure was not persisted: %q %q %d", shareStatus, cleanupError, attempts)
	}
	if _, err := svc.db.ExecContext(ctx, `UPDATE upload_sessions SET backend=?,storage_profile_id=?,s3_upload_id=NULL WHERE id=?`, originalBackend, originalProfile, upload.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.db.ExecContext(ctx, "UPDATE shares SET cleanup_next_attempt_at=0 WHERE id=?", creation.Share.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.RunMaintenanceOnce(ctx); err != nil {
		t.Fatal(err)
	}
	var taskStatus string
	if err := svc.db.QueryRowContext(ctx, "SELECT status FROM upload_tasks WHERE share_id=?", creation.Share.ID).Scan(&taskStatus); err != nil {
		t.Fatal(err)
	}
	if taskStatus != "aborted" {
		t.Fatalf("task status after successful retry = %s", taskStatus)
	}
	if err := svc.db.QueryRowContext(ctx, "SELECT status,COALESCE(cleanup_error,''),cleanup_attempts FROM shares WHERE id=?", creation.Share.ID).Scan(&shareStatus, &cleanupError, &attempts); err != nil {
		t.Fatal(err)
	}
	if shareStatus != "metadata_only" || cleanupError != "" || attempts != 0 {
		t.Fatalf("cleanup success did not clear retry state: %q %q %d", shareStatus, cleanupError, attempts)
	}
}

func TestPreviewPolicyBlocksActiveContent(t *testing.T) {
	var contract struct {
		Previews []struct {
			MIME    string `json:"mime"`
			Name    string `json:"name"`
			Allowed bool   `json:"allowed"`
		} `json:"previews"`
	}
	data, err := os.ReadFile(filepath.Join("..", "..", "tests", "contracts", "reliability.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &contract); err != nil {
		t.Fatal(err)
	}
	for _, test := range contract.Previews {
		if got := PreviewAllowed(test.MIME, test.Name); got != test.Allowed {
			t.Errorf("PreviewAllowed(%q, %q) = %t, want %t", test.MIME, test.Name, got, test.Allowed)
		}
	}
}

func TestLocalShareLifecycle(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t)
	current := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return current }

	maxUses := 1
	invite, err := svc.CreateInvite(ctx, "test", &maxUses, nil)
	if err != nil {
		t.Fatal(err)
	}
	downloadLimit := 2
	creation, err := svc.CreateShare(ctx, CreateShareInput{
		InviteCode: invite.Code, Name: "项目交付资料", ExpiresIn: "168h", DownloadLimit: &downloadLimit,
		Entries: []core.ManifestEntry{
			{Path: "项目/设计稿.txt", Kind: "file", Size: 5, MIMEType: "text/plain"},
			{Path: "项目/空文件.bin", Kind: "file", Size: 0, MIMEType: "application/octet-stream"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(creation.Uploads) != 2 || creation.Share.Status != "uploading" {
		t.Fatalf("unexpected creation: %#v", creation)
	}
	if _, err := svc.GetPublicShare(ctx, creation.PublicToken, true); err != core.ErrConflict {
		t.Fatalf("uploading share should not be public: %v", err)
	}

	for _, upload := range creation.Uploads {
		if upload.Path == "项目/设计稿.txt" {
			if _, err := svc.PutLocalPart(ctx, upload.SessionID, upload.Token, 1, bytes.NewBufferString("hello")); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := svc.CompleteUpload(ctx, upload.SessionID, upload.Token, CompleteUploadInput{}); err != nil {
			t.Fatalf("complete %s: %v", upload.Path, err)
		}
	}
	public, err := svc.GetPublicShare(ctx, creation.PublicToken, true)
	if err != nil {
		t.Fatal(err)
	}
	if public.Share.Status != "active" || len(public.Entries) != 3 {
		t.Fatalf("unexpected public share: %#v", public)
	}
	var textEntry core.Entry
	for _, entry := range public.Entries {
		if entry.Path == "项目/设计稿.txt" {
			textEntry = entry
		}
	}
	grant, err := svc.CreateDownloadGrant(ctx, creation.PublicToken, true, textEntry.ID, "file")
	if err != nil {
		t.Fatal(err)
	}
	resource, err := svc.ResolveDownload(ctx, grant.Token)
	if err != nil {
		t.Fatal(err)
	}
	object, err := svc.OpenResource(ctx, resource, &storage.ByteRange{Start: 1, End: 3})
	if err != nil {
		t.Fatal(err)
	}
	content, err := io.ReadAll(object.Body)
	object.Body.Close()
	if err != nil || string(content) != "ell" {
		t.Fatalf("unexpected downloaded range: %q %v", content, err)
	}
	if _, err := svc.ResolveDownload(ctx, grant.Token); err != nil {
		t.Fatal(err)
	}
	managed, err := svc.GetManagedShare(ctx, creation.ManagementToken)
	if err != nil {
		t.Fatal(err)
	}
	if managed.Share.DownloadCount != 1 {
		t.Fatalf("grant retries must not increase count: %d", managed.Share.DownloadCount)
	}

	if err := svc.RevokeManagedShare(ctx, creation.ManagementToken); err != nil {
		t.Fatal(err)
	}
	if err := svc.RunMaintenanceOnce(ctx); err != nil {
		t.Fatal(err)
	}
	managed, err = svc.GetManagedShare(ctx, creation.ManagementToken)
	if err != nil {
		t.Fatal(err)
	}
	if managed.Share.Status != "revoked" {
		t.Fatalf("active grant should delay cleanup: %s", managed.Share.Status)
	}

	current = current.Add(16 * time.Minute)
	if err := svc.RunMaintenanceOnce(ctx); err != nil {
		t.Fatal(err)
	}
	managed, err = svc.GetManagedShare(ctx, creation.ManagementToken)
	if err != nil {
		t.Fatal(err)
	}
	if managed.Share.Status != "metadata_only" || len(managed.Entries) != 3 {
		t.Fatalf("metadata should remain after content cleanup: %#v", managed)
	}
	if _, err := svc.OpenResource(ctx, resource, nil); err == nil {
		t.Fatal("content should have been deleted")
	}

	current = current.Add(30*24*time.Hour + time.Second)
	if err := svc.RunMaintenanceOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GetManagedShare(ctx, creation.ManagementToken); err != core.ErrNotFound {
		t.Fatalf("metadata should be deleted after 30 days: %v", err)
	}
}

func TestConcurrentNestedLocalUploads(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t)
	maxUses := 1
	invite, err := svc.CreateInvite(ctx, "nested concurrent upload", &maxUses, nil)
	if err != nil {
		t.Fatal(err)
	}
	creation, err := svc.CreateShare(ctx, CreateShareInput{
		InviteCode: invite.Code, Name: "nested folder", ExpiresIn: "24h",
		Entries: []core.ManifestEntry{
			{Path: "root/root.txt", Kind: "file", Size: 4, MIMEType: "text/plain"},
			{Path: "root/level1/middle.txt", Kind: "file", Size: 4, MIMEType: "text/plain"},
			{Path: "root/level1/level2/deep.txt", Kind: "file", Size: 4, MIMEType: "text/plain"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	errorsByPath := make(chan error, len(creation.Uploads))
	var uploads sync.WaitGroup
	for _, descriptor := range creation.Uploads {
		descriptor := descriptor
		uploads.Add(1)
		go func() {
			defer uploads.Done()
			<-start
			if _, err := svc.PutLocalPart(ctx, descriptor.SessionID, descriptor.Token, 1, bytes.NewBufferString("data")); err != nil {
				errorsByPath <- fmt.Errorf("put %s: %w", descriptor.Path, err)
				return
			}
			if _, err := svc.CompleteUpload(ctx, descriptor.SessionID, descriptor.Token, CompleteUploadInput{}); err != nil {
				errorsByPath <- fmt.Errorf("complete %s: %w", descriptor.Path, err)
			}
		}()
	}
	close(start)
	uploads.Wait()
	close(errorsByPath)
	for uploadErr := range errorsByPath {
		t.Error(uploadErr)
	}
	if t.Failed() {
		return
	}
	public, err := svc.GetPublicShare(ctx, creation.PublicToken, true)
	if err != nil {
		t.Fatal(err)
	}
	if public.Share.Status != "active" || len(public.Entries) != 6 {
		t.Fatalf("unexpected nested share: status=%s entries=%d", public.Share.Status, len(public.Entries))
	}
}

func TestBackupAndRuntimeSettings(t *testing.T) {
	ctx := context.Background()
	svc, cfg := newTestService(t)
	current := time.Date(2026, 7, 26, 4, 0, 0, 0, time.Local)
	svc.now = func() time.Time { return current }
	limit := int64(12 * 1024 * 1024 * 1024)
	settings, err := svc.UpdateSettings(ctx, UpdateRuntimeSettings{MaxFileBytes: &limit})
	if err != nil {
		t.Fatal(err)
	}
	if settings.MaxFileBytes != limit || svc.RuntimeSettings().MaxFileBytes != limit {
		t.Fatal("runtime setting was not applied")
	}
	if err := svc.RunBackupIfDue(ctx); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(cfg.Backup.Directory, "sharepipe-*.db"))
	if err != nil || len(files) != 1 {
		t.Fatalf("expected one backup: %#v %v", files, err)
	}
	if info, err := os.Stat(files[0]); err != nil || info.Size() == 0 {
		t.Fatalf("backup is invalid: %v", err)
	}
}

func TestRuntimeSettingsRejectInvalidInputAndMergeConcurrentUpdates(t *testing.T) {
	ctx := context.Background()
	svc, cfg := newTestService(t)
	original := svc.RuntimeSettings()
	invalid := int64(1)
	if _, err := svc.UpdateSettings(ctx, UpdateRuntimeSettings{MaxFileBytes: &invalid}); !errors.Is(err, core.ErrValidation) {
		t.Fatalf("invalid setting error = %v", err)
	}
	if current := svc.RuntimeSettings(); current != original {
		t.Fatalf("invalid update changed runtime settings: %#v", current)
	}

	fileLimit := cfg.Uploads.ChunkSizeBytes * 2
	downloadLimit := 7
	start := make(chan struct{})
	errorsByUpdate := make(chan error, 2)
	var updates sync.WaitGroup
	updates.Add(2)
	go func() {
		defer updates.Done()
		<-start
		_, err := svc.UpdateSettings(ctx, UpdateRuntimeSettings{MaxFileBytes: &fileLimit})
		errorsByUpdate <- err
	}()
	go func() {
		defer updates.Done()
		<-start
		_, err := svc.UpdateSettings(ctx, UpdateRuntimeSettings{DefaultDownloadLimit: &downloadLimit})
		errorsByUpdate <- err
	}()
	close(start)
	updates.Wait()
	close(errorsByUpdate)
	for updateErr := range errorsByUpdate {
		if updateErr != nil {
			t.Fatal(updateErr)
		}
	}
	current := svc.RuntimeSettings()
	if current.MaxFileBytes != fileLimit || current.DefaultDownloadLimit != downloadLimit {
		t.Fatalf("concurrent settings were not merged: %#v", current)
	}
}
