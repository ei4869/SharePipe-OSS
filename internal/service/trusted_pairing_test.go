package service

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ei4869/SharePipe-OSS/internal/core"
)

func TestTrustedInviteEnrollsOnceAndRevocationBlocksFutureAuthorization(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t)
	now := time.Date(2026, 7, 28, 4, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	invite, err := svc.CreateInviteWithKind(ctx, "friend", nil, nil, "trusted_device", "小明的电脑")
	if err != nil {
		t.Fatal(err)
	}
	creation, err := svc.CreateShare(ctx, CreateShareInput{InviteCode: invite.Code, Name: "first", ExpiresIn: "24h", PairingEnabled: true, QREnabled: true, RemoteIP: "192.0.2.4", UserAgent: "test", Platform: "Windows", Entries: []core.ManifestEntry{{Path: "one.txt", Kind: "file", Size: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	if creation.TrustedDeviceToken == "" || creation.TrustedDeviceName != "小明的电脑" {
		t.Fatalf("trusted credential was not issued: %#v", creation)
	}
	if _, err := svc.CreateShare(ctx, CreateShareInput{InviteCode: invite.Code, Name: "reused", ExpiresIn: "24h", Entries: []core.ManifestEntry{{Path: "two.txt", Kind: "file", Size: 1}}}); !errors.Is(err, core.ErrUnauthorized) {
		t.Fatalf("trusted invite was reusable: %v", err)
	}
	auth, err := svc.AuthorizeTrustedDevice(ctx, creation.TrustedDeviceToken, "192.0.2.4")
	if err != nil || auth.Device == nil {
		t.Fatalf("trusted device authorization failed: %#v %v", auth, err)
	}
	if err := svc.RevokeTrustedDevice(ctx, auth.Device.ID); err != nil {
		t.Fatal(err)
	}
	auth, err = svc.AuthorizeTrustedDevice(ctx, creation.TrustedDeviceToken, "192.0.2.4")
	if err != nil || auth.Device != nil {
		t.Fatalf("revoked device remained authorized: %#v %v", auth, err)
	}
}

func TestTrustedInviteDefaultsItsLabelToTheDeviceName(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t)
	invite, err := svc.CreateInviteWithKind(ctx, "", nil, nil, "trusted_device", "小明的电脑")
	if err != nil {
		t.Fatal(err)
	}
	if invite.Label != "小明的电脑" || invite.MaxUses == nil || *invite.MaxUses != 1 {
		t.Fatalf("trusted invitation did not inherit its device name: %#v", invite)
	}
}

func TestPairCodeStartsAtActivationSupportsMultipleRecipientsAndExistingAccessSurvivesCodeExpiry(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t)
	now := time.Date(2026, 7, 28, 4, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	invite, err := svc.CreateInvite(ctx, "pair", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	creation, err := svc.CreateShare(ctx, CreateShareInput{InviteCode: invite.Code, Name: "paired", ExpiresIn: "24h", PairingEnabled: true, QREnabled: true, Entries: []core.ManifestEntry{{Path: "one.txt", Kind: "file", Size: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	before, err := svc.PairingForManagedShare(ctx, creation.Share.ID)
	if err != nil || before.Code != "" {
		t.Fatalf("pair code started before activation: %#v %v", before, err)
	}
	upload := creation.Uploads[0]
	if _, err := svc.PutLocalPart(ctx, upload.SessionID, upload.Token, 1, bytes.NewBufferString("x")); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CompleteUpload(ctx, upload.SessionID, upload.Token, CompleteUploadInput{}); err != nil {
		t.Fatal(err)
	}
	active, err := svc.PairingForManagedShare(ctx, creation.Share.ID)
	if err != nil || len(active.Code) != 6 || active.ExpiresAt == nil || !active.ExpiresAt.Equal(now.Add(10*time.Minute)) {
		t.Fatalf("pair code activation is invalid: %#v %v", active, err)
	}
	firstURL, _, err := svc.ResolvePairCode(ctx, active.Code)
	if err != nil {
		t.Fatal(err)
	}
	secondURL, _, err := svc.ResolvePairCode(ctx, active.Code)
	if err != nil {
		t.Fatal(err)
	}
	if firstURL == secondURL {
		t.Fatal("pairing recipients received the same access token")
	}
	firstToken := firstURL[len(firstURL)-43:]
	now = now.Add(11 * time.Minute)
	if _, _, err := svc.ResolvePairCode(ctx, active.Code); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("expired pair code resolved: %v", err)
	}
	if _, err := svc.GetPublicShare(ctx, firstToken, true); err != nil {
		t.Fatalf("redeemed access expired with pair code: %v", err)
	}
}

func TestLocalUploadRecoveryUsesManagementTokenAndRestoresParts(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t)
	creation, err := svc.CreateShare(ctx, CreateShareInput{
		Name: "recoverable", ExpiresIn: "24h", LocalSystem: true, PairingEnabled: true, QREnabled: true,
		Entries: []core.ManifestEntry{{Path: "recover.txt", Kind: "file", Size: 4, MIMEType: "text/plain"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if creation.RecoveryURL == "" || creation.Uploads[0].Parts == nil || creation.Uploads[0].ExpectedSize != 4 {
		t.Fatalf("initial upload descriptor is incomplete: %#v", creation)
	}
	upload := creation.Uploads[0]
	if _, err := svc.PutLocalPart(ctx, upload.SessionID, creation.ManagementToken, 1, bytes.NewBufferString("data")); err != nil {
		t.Fatalf("management token could not resume a part: %v", err)
	}
	recovered, err := svc.RecoverUpload(ctx, creation.ManagementToken)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.PublicToken != creation.PublicToken || len(recovered.Uploads) != 1 || len(recovered.Uploads[0].Parts) != 1 {
		t.Fatalf("upload state was not recovered: %#v", recovered)
	}
	if recovered.Uploads[0].Token != creation.ManagementToken || recovered.Uploads[0].Parts[0].SHA256 == "" {
		t.Fatalf("recovered upload credentials or checksum are invalid: %#v", recovered.Uploads[0])
	}
	if _, err := svc.CompleteUpload(ctx, upload.SessionID, creation.ManagementToken, CompleteUploadInput{}); err != nil {
		t.Fatal(err)
	}
}
