package service

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ei4869/SharePipe-OSS/internal/core"
)

func TestAdministratorEmailsAndPasswordAreRuntimeManaged(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	svc.cfg.Admin.InitialEmails = []string{"bootstrap@example.com"}
	if err := svc.initializeAdministrator(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddAdminEmail(ctx, "Admin@Example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddAdminEmail(ctx, "second@example.com"); err != nil {
		t.Fatal(err)
	}
	if !svc.AdminEmailAllowed(ctx, "admin@example.com") {
		t.Fatal("normalized administrator email was not authorized")
	}
	before := svc.AdminPasswordVersion(ctx)
	if err := svc.ChangeAdminPassword(ctx, "", "a memorable admin passphrase", false); err != nil {
		t.Fatal(err)
	}
	if !svc.AuthenticateAdmin("a memorable admin passphrase") {
		t.Fatal("changed administrator password did not authenticate")
	}
	if svc.AdminPasswordVersion(ctx) != before+1 {
		t.Fatal("password version did not invalidate older sessions")
	}
	if err := svc.RemoveAdminEmail(ctx, "admin@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := svc.RemoveAdminEmail(ctx, "bootstrap@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := svc.initializeAdministrator(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if svc.AdminEmailAllowed(ctx, "bootstrap@example.com") {
		t.Fatal("deleted bootstrap email was restored during reinitialization")
	}
	if err := svc.RemoveAdminEmail(ctx, "second@example.com"); !errors.Is(err, core.ErrConflict) {
		t.Fatalf("removing the final administrator email error = %v", err)
	}
}

func TestStorageProfilesKeepCredentialsEncryptedAndSwitchDefault(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "secondary")
	profile, err := svc.CreateStorageProfile(ctx, StorageProfileInput{
		Name: "Secondary disk", Provider: "local",
		Config: StorageProfileConfig{RootPath: root},
	})
	if err != nil {
		t.Fatal(err)
	}
	if profile.Kind != "local" || profile.Config.RootPath != filepath.Clean(root) {
		t.Fatalf("unexpected local profile: %+v", profile)
	}
	if health := svc.TestStorageProfile(ctx, profile.ID); !health.Healthy {
		t.Fatalf("local storage health failed: %s", health.Error)
	}
	if err := svc.SetDefaultStorage(ctx, profile.ID); err != nil {
		t.Fatal(err)
	}
	if got, err := svc.DefaultStorage(ctx); err != nil || got != profile.ID {
		t.Fatalf("default profile = %q, %v", got, err)
	}

	const accessKey = "example-access-key"
	const secretKey = "example-secret-key"
	s3Profile, err := svc.CreateStorageProfile(ctx, StorageProfileInput{
		Name: "Contract S3", Provider: "generic-s3",
		Config: StorageProfileConfig{
			Endpoint: "https://s3.example.test", Region: "test-1", Bucket: "sharepipe-test",
			Prefix: "sharepipe", PathStyle: true, ServerSideEncryption: "none",
		},
		Credentials: StorageCredentials{AccessKeyID: accessKey, SecretAccessKey: secretKey},
	})
	if err != nil {
		t.Fatal(err)
	}
	var encrypted string
	if err := svc.db.QueryRowContext(ctx, "SELECT credentials_encrypted FROM storage_profiles WHERE id=?", s3Profile.ID).Scan(&encrypted); err != nil {
		t.Fatal(err)
	}
	if encrypted == "" || strings.Contains(encrypted, accessKey) || strings.Contains(encrypted, secretKey) {
		t.Fatal("storage credentials were not encrypted at rest")
	}
	listed, err := svc.ListStorageProfiles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range listed {
		encoded := item.Name + item.Config.Endpoint
		if strings.Contains(encoded, accessKey) || strings.Contains(encoded, secretKey) {
			t.Fatal("storage credentials leaked through profile listing")
		}
	}

	if _, err := svc.UpdateStorageProfile(ctx, s3Profile.ID, StorageProfileInput{
		Name: s3Profile.Name, Provider: s3Profile.Provider, Config: s3Profile.Config,
		Credentials: StorageCredentials{AccessKeyID: "replacement-without-secret"},
	}); !errors.Is(err, core.ErrValidation) {
		t.Fatalf("partial credential replacement error = %v", err)
	}

	withSession, err := svc.UpdateStorageProfile(ctx, s3Profile.ID, StorageProfileInput{
		Name: s3Profile.Name, Provider: s3Profile.Provider, Config: s3Profile.Config,
		Credentials: StorageCredentials{SessionToken: "temporary-session-token"},
	})
	if err != nil {
		t.Fatal(err)
	}
	cleared, err := svc.UpdateStorageProfile(ctx, withSession.ID, StorageProfileInput{
		Name: withSession.Name, Provider: withSession.Provider, Config: withSession.Config,
		Credentials: StorageCredentials{ClearSessionToken: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	record, err := svc.storageProfileRecord(ctx, cleared.ID)
	if err != nil {
		t.Fatal(err)
	}
	credentials, err := svc.decryptStorageCredentials(cleared.ID, record.credentialsEncrypted)
	if err != nil {
		t.Fatal(err)
	}
	if credentials.SessionToken != "" || credentials.ClearSessionToken {
		t.Fatal("temporary S3 session token was not cleared")
	}
}

func TestStorageProfileProviderSafetyRules(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	local, err := svc.storageProfile(ctx, "local")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateStorageProfile(ctx, "local", StorageProfileInput{
		Name: local.Name, Provider: "nas", Config: local.Config,
	}); !errors.Is(err, core.ErrConflict) {
		t.Fatalf("built-in local provider change error = %v", err)
	}
	if _, err := svc.CreateStorageProfile(ctx, StorageProfileInput{
		Name: "Invalid R2 encryption", Provider: "cloudflare-r2",
		Config: StorageProfileConfig{
			AccountID: strings.Repeat("a", 32), Bucket: "test", Prefix: "sharepipe",
			ServerSideEncryption: "AES256",
		},
		Credentials: StorageCredentials{AccessKeyID: "key", SecretAccessKey: "secret"},
	}); !errors.Is(err, core.ErrValidation) {
		t.Fatalf("R2 incompatible encryption error = %v", err)
	}
	if _, err := svc.CreateStorageProfile(ctx, StorageProfileInput{
		Name: "Escaping prefix", Provider: "generic-s3",
		Config: StorageProfileConfig{
			Endpoint: "https://s3.example.test", Region: "test-1", Bucket: "test", Prefix: "../outside",
		},
		Credentials: StorageCredentials{AccessKeyID: "key", SecretAccessKey: "secret"},
	}); !errors.Is(err, core.ErrValidation) {
		t.Fatalf("escaping storage prefix error = %v", err)
	}
	if _, err := svc.CreateStorageProfile(ctx, StorageProfileInput{
		Name: "AWS role credentials", Provider: "aws-s3",
		Config: StorageProfileConfig{Region: "ap-southeast-1", Bucket: "role-backed-bucket", Prefix: "sharepipe"},
	}); err != nil {
		t.Fatalf("AWS default credential chain profile error = %v", err)
	}
}

func TestDeletedBootstrapS3ProfileIsNotRestored(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	now := svc.now().Unix()
	config := StorageProfileConfig{
		Endpoint: "https://s3.example.test", Region: "test-1", Bucket: "test", Prefix: "sharepipe",
		PathStyle: true, ServerSideEncryption: "none",
	}
	credentials := StorageCredentials{AccessKeyID: "key", SecretAccessKey: "secret"}
	if err := svc.insertBootstrapStorageProfile(ctx, "s3", "Bootstrap S3", "s3", "generic-s3", config, credentials, now); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteStorageProfile(ctx, "s3"); err != nil {
		t.Fatal(err)
	}
	svc.cfg.Storage.S3.Enabled = true
	svc.cfg.Storage.S3.Endpoint = config.Endpoint
	svc.cfg.Storage.S3.Region = config.Region
	svc.cfg.Storage.S3.Bucket = config.Bucket
	svc.cfg.Storage.S3.Prefix = config.Prefix
	svc.cfg.Storage.S3.PathStyle = true
	svc.cfg.Storage.S3.ServerSideEncryption = "none"
	svc.cfg.S3AccessKeyID = credentials.AccessKeyID
	svc.cfg.S3SecretAccessKey = credentials.SecretAccessKey
	if err := svc.initializeStorageProfiles(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.storageProfile(ctx, "s3"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("deleted bootstrap S3 profile was restored: %v", err)
	}
}
