package storage

import (
	"context"
	"testing"
)

func TestS3ObjectKeyBoundary(t *testing.T) {
	store, err := NewS3(context.Background(), S3Options{
		Endpoint: "http://127.0.0.1:9000", Region: "us-east-1", Bucket: "sharepipe-test",
		Prefix: "tenant", PathStyle: true, AccessKeyID: "test", SecretAccessKey: "testtest",
	})
	if err != nil {
		t.Fatal(err)
	}
	key, err := store.objectKey("shares/a/object")
	if err != nil || key != "tenant/shares/a/object" {
		t.Fatalf("unexpected key %q: %v", key, err)
	}
	if _, err := store.objectKey("../escape"); err == nil {
		t.Fatal("expected traversal to fail")
	}
}

func TestContentRangeTotal(t *testing.T) {
	total, ok := totalFromContentRange("bytes 10-19/100")
	if !ok || total != 100 {
		t.Fatalf("unexpected total: %d %v", total, ok)
	}
}
