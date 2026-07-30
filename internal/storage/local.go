package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Local struct {
	root string
}

func NewLocal(root string) (*Local, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve local storage root: %w", err)
	}
	if err := os.MkdirAll(absolute, 0o750); err != nil {
		return nil, fmt.Errorf("create local storage root: %w", err)
	}
	return &Local{root: filepath.Clean(absolute)}, nil
}

func (l *Local) Name() string { return "local" }

func (l *Local) Put(ctx context.Context, key string, body io.Reader, size int64) (ObjectInfo, error) {
	target, err := l.safePath(key)
	if err != nil {
		return ObjectInfo{}, err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		return ObjectInfo{}, fmt.Errorf("create object directory: %w", err)
	}
	if _, err := os.Stat(target); err == nil {
		return ObjectInfo{}, fmt.Errorf("object already exists: %s", key)
	} else if !errors.Is(err, os.ErrNotExist) {
		return ObjectInfo{}, fmt.Errorf("inspect object target: %w", err)
	}
	temp, err := os.CreateTemp(filepath.Dir(target), ".sharepipe-*")
	if err != nil {
		return ObjectInfo{}, fmt.Errorf("create staged object: %w", err)
	}
	tempName := temp.Name()
	committed := false
	defer func() {
		temp.Close()
		if !committed {
			_ = os.Remove(tempName)
		}
	}()
	hash := sha256.New()
	reader := io.TeeReader(body, hash)
	written, err := copyWithContext(ctx, temp, reader)
	if err != nil {
		return ObjectInfo{}, fmt.Errorf("write object: %w", err)
	}
	if size >= 0 && written != size {
		return ObjectInfo{}, fmt.Errorf("object size mismatch: expected %d, wrote %d", size, written)
	}
	if err := temp.Sync(); err != nil {
		return ObjectInfo{}, fmt.Errorf("sync object: %w", err)
	}
	if err := temp.Close(); err != nil {
		return ObjectInfo{}, fmt.Errorf("close object: %w", err)
	}
	if err := os.Rename(tempName, target); err != nil {
		return ObjectInfo{}, fmt.Errorf("commit object: %w", err)
	}
	committed = true
	info, err := os.Stat(target)
	if err != nil {
		return ObjectInfo{}, fmt.Errorf("stat committed object: %w", err)
	}
	return ObjectInfo{Size: info.Size(), ModTime: info.ModTime(), ETag: hex.EncodeToString(hash.Sum(nil))}, nil
}

func (l *Local) Open(_ context.Context, key string, byteRange *ByteRange) (Object, error) {
	target, err := l.safePath(key)
	if err != nil {
		return Object{}, err
	}
	file, err := os.Open(target)
	if err != nil {
		return Object{}, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return Object{}, err
	}
	total := info.Size()
	if byteRange == nil {
		return Object{Body: file, Size: total, ContentLength: total, ModTime: info.ModTime()}, nil
	}
	if byteRange.Start < 0 || byteRange.End < byteRange.Start || byteRange.Start >= total {
		file.Close()
		return Object{}, errors.New("invalid byte range")
	}
	end := byteRange.End
	if end >= total {
		end = total - 1
	}
	length := end - byteRange.Start + 1
	section := io.NewSectionReader(file, byteRange.Start, length)
	return Object{
		Body: &sectionReadCloser{SectionReader: section, closer: file},
		Size: total, ContentLength: length, ModTime: info.ModTime(),
	}, nil
}

func (l *Local) Stat(_ context.Context, key string) (ObjectInfo, error) {
	target, err := l.safePath(key)
	if err != nil {
		return ObjectInfo{}, err
	}
	info, err := os.Stat(target)
	if err != nil {
		return ObjectInfo{}, err
	}
	return ObjectInfo{Size: info.Size(), ModTime: info.ModTime()}, nil
}

func (l *Local) Delete(_ context.Context, key string) error {
	target, err := l.safePath(key)
	if err != nil {
		return err
	}
	if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (l *Local) DeletePrefix(_ context.Context, prefix string) error {
	target, err := l.safePath(prefix)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(l.root, target)
	if err != nil || relative == "." {
		return errors.New("refusing to delete local storage root")
	}
	if err := os.RemoveAll(target); err != nil {
		return fmt.Errorf("delete object prefix: %w", err)
	}
	return nil
}

func (l *Local) Root() string { return l.root }

func (l *Local) safePath(key string) (string, error) {
	key = strings.TrimSpace(strings.ReplaceAll(key, "\\", "/"))
	if key == "" || strings.HasPrefix(key, "/") || strings.Contains(key, ":") {
		return "", errors.New("invalid object key")
	}
	cleaned := filepath.Clean(filepath.FromSlash(key))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", errors.New("object key escapes storage root")
	}
	target := filepath.Join(l.root, cleaned)
	relative, err := filepath.Rel(l.root, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("object key escapes storage root")
	}
	return target, nil
}

type sectionReadCloser struct {
	*io.SectionReader
	closer io.Closer
}

func (s *sectionReadCloser) Close() error { return s.closer.Close() }

func copyWithContext(ctx context.Context, destination io.Writer, source io.Reader) (int64, error) {
	buffer := make([]byte, 128*1024)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		count, readErr := source.Read(buffer)
		if count > 0 {
			written, writeErr := destination.Write(buffer[:count])
			total += int64(written)
			if writeErr != nil {
				return total, writeErr
			}
			if written != count {
				return total, io.ErrShortWrite
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

func ObjectKey(shareID, entryID string) string {
	return filepath.ToSlash(filepath.Join("shares", shareID, "objects", entryID))
}

func ZipKey(shareID string, created time.Time) string {
	return filepath.ToSlash(filepath.Join("shares", shareID, "zip", created.UTC().Format("20060102T150405Z")+".zip"))
}
