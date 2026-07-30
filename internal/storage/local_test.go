package storage

import (
	"bytes"
	"context"
	"io"
	"testing"
)

func TestLocalPutOpenRangeAndDeletePrefix(t *testing.T) {
	store, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	info, err := store.Put(ctx, "shares/a/objects/b", bytes.NewBufferString("0123456789"), 10)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size != 10 || info.ETag == "" {
		t.Fatalf("unexpected object info: %#v", info)
	}
	object, err := store.Open(ctx, "shares/a/objects/b", &ByteRange{Start: 2, End: 5})
	if err != nil {
		t.Fatal(err)
	}
	content, err := io.ReadAll(object.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := object.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if string(content) != "2345" || object.ContentLength != 4 || object.Size != 10 {
		t.Fatalf("unexpected range response: %q %#v", content, object)
	}
	if err := store.DeletePrefix(ctx, "shares/a"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Stat(ctx, "shares/a/objects/b"); err == nil {
		t.Fatal("expected object to be deleted")
	}
}

func TestLocalRejectsTraversal(t *testing.T) {
	store, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"../escape", "/absolute", "C:/drive", ""} {
		if _, err := store.Put(context.Background(), key, bytes.NewReader(nil), 0); err == nil {
			t.Fatalf("expected key %q to be rejected", key)
		}
	}
}
