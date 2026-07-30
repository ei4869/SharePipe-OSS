package core

import (
	"errors"
	"time"
)

var (
	ErrNotFound         = errors.New("not found")
	ErrUnauthorized     = errors.New("unauthorized")
	ErrForbidden        = errors.New("forbidden")
	ErrConflict         = errors.New("conflict")
	ErrExpired          = errors.New("expired")
	ErrDownloadLimit    = errors.New("download limit reached")
	ErrValidation       = errors.New("validation failed")
	ErrUploadIncomplete = errors.New("upload incomplete")
	ErrUnsafeUpload     = errors.New("unsafe upload rejected")
)

type ManifestEntry struct {
	Path     string `json:"path"`
	Kind     string `json:"kind"`
	Size     int64  `json:"size"`
	MIMEType string `json:"mimeType"`
}

type Share struct {
	ID                  string     `json:"id"`
	Name                string     `json:"name"`
	Description         string     `json:"description"`
	StorageBackend      string     `json:"storageBackend"`
	StorageProfileID    string     `json:"storageProfileId,omitempty"`
	Status              string     `json:"status"`
	ExpiresAt           time.Time  `json:"expiresAt"`
	DownloadLimit       *int       `json:"downloadLimit"`
	DownloadCount       int        `json:"downloadCount"`
	FileCount           int        `json:"fileCount"`
	TotalBytes          int64      `json:"totalBytes"`
	PasswordProtected   bool       `json:"passwordProtected"`
	ContentDeletedAt    *time.Time `json:"contentDeletedAt,omitempty"`
	MetadataDeleteAfter *time.Time `json:"metadataDeleteAfter,omitempty"`
	CreatedAt           time.Time  `json:"createdAt"`
	UpdatedAt           time.Time  `json:"updatedAt"`
	PairingEnabled      bool       `json:"pairingEnabled"`
	QREnabled           bool       `json:"qrEnabled"`
}

type Entry struct {
	ID         string    `json:"id"`
	ShareID    string    `json:"-"`
	Path       string    `json:"path"`
	Kind       string    `json:"kind"`
	Size       int64     `json:"size"`
	MIMEType   string    `json:"mimeType"`
	StorageKey string    `json:"-"`
	SHA256     string    `json:"sha256,omitempty"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"createdAt"`
}

type UploadDescriptor struct {
	SessionID     string       `json:"sessionId"`
	EntryID       string       `json:"entryId"`
	Path          string       `json:"path"`
	Token         string       `json:"token"`
	Backend       string       `json:"backend"`
	ChunkSize     int64        `json:"chunkSize"`
	PartCount     int          `json:"partCount"`
	ExpectedSize  int64        `json:"expectedSize"`
	ReceivedBytes int64        `json:"receivedBytes"`
	Status        string       `json:"status"`
	Parts         []UploadPart `json:"parts"`
	UploadID      string       `json:"-"`
}

type UploadPart struct {
	PartNumber int    `json:"partNumber"`
	Size       int64  `json:"size"`
	ETag       string `json:"etag"`
	SHA256     string `json:"sha256"`
}

type ShareCreation struct {
	Share              Share              `json:"share"`
	PublicToken        string             `json:"publicToken"`
	ManagementToken    string             `json:"managementToken"`
	PublicURL          string             `json:"publicUrl"`
	ManagementURL      string             `json:"managementUrl"`
	RecoveryURL        string             `json:"recoveryUrl"`
	UploadTaskStatus   string             `json:"uploadTaskStatus"`
	Uploads            []UploadDescriptor `json:"uploads"`
	TrustedDeviceToken string             `json:"-"`
	TrustedDeviceName  string             `json:"-"`
}

type UploadTask struct {
	ShareID   string    `json:"shareId"`
	Status    string    `json:"status"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type PublicShare struct {
	Share   Share         `json:"share"`
	Entries []Entry       `json:"entries"`
	Locked  bool          `json:"locked"`
	Pairing *PairingState `json:"pairing,omitempty"`
}

type DownloadGrant struct {
	Token     string    `json:"token"`
	URL       string    `json:"url,omitempty"`
	Status    string    `json:"status"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type Invite struct {
	ID         string     `json:"id"`
	Code       string     `json:"code,omitempty"`
	Label      string     `json:"label"`
	MaxUses    *int       `json:"maxUses"`
	Uses       int        `json:"uses"`
	ExpiresAt  *time.Time `json:"expiresAt"`
	RevokedAt  *time.Time `json:"revokedAt"`
	CreatedAt  time.Time  `json:"createdAt"`
	Kind       string     `json:"kind"`
	DeviceName string     `json:"deviceName"`
}

type TrustedDevice struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Platform   string     `json:"platform"`
	UserAgent  string     `json:"userAgent"`
	ExpiresAt  time.Time  `json:"expiresAt"`
	LastSeenAt time.Time  `json:"lastSeenAt"`
	RevokedAt  *time.Time `json:"revokedAt,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	System     bool       `json:"system"`
	Scope      string     `json:"scope"`
}

type PairingState struct {
	Enabled   bool       `json:"enabled"`
	Code      string     `json:"code,omitempty"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	Expired   bool       `json:"expired,omitempty"`
}
