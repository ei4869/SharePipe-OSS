package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultsValidate(t *testing.T) {
	if err := Defaults().Validate(); err != nil {
		t.Fatalf("defaults should be valid: %v", err)
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"unexpected":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected unknown field error")
	}
}

func TestResolvePaths(t *testing.T) {
	cfg := Defaults().ResolvePaths(t.TempDir())
	if !filepath.IsAbs(cfg.Database.Path) {
		t.Fatalf("database path should be absolute: %s", cfg.Database.Path)
	}
}

func TestExpiryAllowlist(t *testing.T) {
	cfg := Defaults()
	if duration, ok := cfg.ExpiryAllowed("168h"); !ok || duration.Hours() != 168 {
		t.Fatalf("expected 168h to be accepted")
	}
	if _, ok := cfg.ExpiryAllowed("2h"); ok {
		t.Fatal("unexpected expiry accepted")
	}
}

func TestValidateRejectsUnsafeNetworkConfiguration(t *testing.T) {
	cfg := Defaults()
	cfg.Server.TrustedProxyCIDRs = []string{"not-a-network"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected invalid trusted proxy CIDR to fail")
	}

	cfg = Defaults()
	cfg.Storage.S3.Enabled = true
	cfg.Storage.S3.Region = "us-east-1"
	cfg.Storage.S3.Bucket = "files"
	cfg.Storage.S3.Endpoint = "javascript:alert(1)"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected invalid S3 endpoint to fail")
	}

	cfg = Defaults()
	cfg.Server.AdminAccess = "hidden-by-obscurity"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected invalid administrator access mode to fail")
	}
}
