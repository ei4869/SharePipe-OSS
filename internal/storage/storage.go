package storage

import (
	"context"
	"io"
	"time"
)

type ByteRange struct {
	Start int64
	End   int64
}

type Object struct {
	Body          io.ReadCloser
	Size          int64
	ContentLength int64
	ModTime       time.Time
	ETag          string
}

type ObjectInfo struct {
	Size    int64
	ModTime time.Time
	ETag    string
}

type Store interface {
	Name() string
	Put(ctx context.Context, key string, body io.Reader, size int64) (ObjectInfo, error)
	Open(ctx context.Context, key string, byteRange *ByteRange) (Object, error)
	Stat(ctx context.Context, key string) (ObjectInfo, error)
	Delete(ctx context.Context, key string) error
	DeletePrefix(ctx context.Context, prefix string) error
}

type MultipartPart struct {
	PartNumber int
	ETag       string
}

type MultipartStore interface {
	Store
	CreateMultipart(ctx context.Context, key, contentType string) (string, error)
	PresignUploadPart(ctx context.Context, key, uploadID string, partNumber int, expires time.Duration) (string, error)
	CompleteMultipart(ctx context.Context, key, uploadID string, parts []MultipartPart) (ObjectInfo, error)
	AbortMultipart(ctx context.Context, key, uploadID string) error
	PresignGet(ctx context.Context, key, filename string, expires time.Duration) (string, error)
}
