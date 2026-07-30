package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Server   ServerConfig   `json:"server"`
	Admin    AdminConfig    `json:"admin"`
	Database DatabaseConfig `json:"database"`
	Storage  StorageConfig  `json:"storage"`
	Uploads  UploadConfig   `json:"uploads"`
	Sharing  SharingConfig  `json:"sharing"`
	Zip      ZipConfig      `json:"zip"`
	Backup   BackupConfig   `json:"backup"`
	Logging  LoggingConfig  `json:"logging"`
	Scanner  ScannerConfig  `json:"scanner"`

	AdminPassword     string `json:"-"`
	AdminPasswordHash string `json:"-"`
	SessionSecret     string `json:"-"`
	StorageMasterKey  string `json:"-"`
	S3AccessKeyID     string `json:"-"`
	S3SecretAccessKey string `json:"-"`
	S3SessionToken    string `json:"-"`
	CloudControlToken string `json:"-"`
}

type ServerConfig struct {
	Listen            string                 `json:"listen"`
	PublicBaseURL     string                 `json:"publicBaseURL"`
	TrustedProxyCIDRs []string               `json:"trustedProxyCIDRs"`
	AdminAccess       string                 `json:"adminAccess"`
	CloudflareAccess  CloudflareAccessConfig `json:"cloudflareAccess"`
}

type CloudflareAccessConfig struct {
	TeamDomain string `json:"teamDomain"`
	Audience   string `json:"audience"`
}

type AdminConfig struct {
	InitialEmails   []string `json:"initialEmails"`
	CloudControlURL string   `json:"cloudControlURL"`
}

type DatabaseConfig struct {
	Path          string `json:"path"`
	BusyTimeoutMS int    `json:"busyTimeoutMs"`
}

type StorageConfig struct {
	DefaultBackend string       `json:"defaultBackend"`
	Local          LocalStorage `json:"local"`
	S3             S3Storage    `json:"s3"`
}

type LocalStorage struct {
	Root string `json:"root"`
}

type S3Storage struct {
	Enabled              bool   `json:"enabled"`
	Endpoint             string `json:"endpoint"`
	Region               string `json:"region"`
	Bucket               string `json:"bucket"`
	Prefix               string `json:"prefix"`
	PathStyle            bool   `json:"pathStyle"`
	ServerSideEncryption string `json:"serverSideEncryption"`
}

type UploadConfig struct {
	ChunkSizeBytes    int64 `json:"chunkSizeBytes"`
	MaxFileBytes      int64 `json:"maxFileBytes"`
	MaxShareBytes     int64 `json:"maxShareBytes"`
	MaxFilesPerShare  int   `json:"maxFilesPerShare"`
	SessionTTLMinutes int   `json:"sessionTTLMinutes"`
}

type SharingConfig struct {
	DefaultExpiry        string   `json:"defaultExpiry"`
	AllowedExpiries      []string `json:"allowedExpiries"`
	DefaultDownloadLimit int      `json:"defaultDownloadLimit"`
	GrantTTLMinutes      int      `json:"grantTTLMinutes"`
	MetadataRetention    string   `json:"metadataRetention"`
}

type ZipConfig struct {
	AsyncThresholdBytes int64  `json:"asyncThresholdBytes"`
	CacheTTL            string `json:"cacheTTL"`
}

type BackupConfig struct {
	Directory      string `json:"directory"`
	Hour           int    `json:"hour"`
	RetentionCount int    `json:"retentionCount"`
}

type LoggingConfig struct {
	Directory           string `json:"directory"`
	DiagnosticRetention string `json:"diagnosticRetention"`
}

type ScannerConfig struct {
	Enabled           bool     `json:"enabled"`
	Command           []string `json:"command"`
	TimeoutSeconds    int      `json:"timeoutSeconds"`
	CleanExitCodes    []int    `json:"cleanExitCodes"`
	InfectedExitCodes []int    `json:"infectedExitCodes"`
	FailClosed        bool     `json:"failClosed"`
}

func Defaults() Config {
	return Config{
		Server: ServerConfig{
			Listen: "127.0.0.1:8080", PublicBaseURL: "http://127.0.0.1:8080", AdminAccess: "local",
		},
		Database: DatabaseConfig{Path: "data/sharepipe.db", BusyTimeoutMS: 5000},
		Storage: StorageConfig{
			DefaultBackend: "local",
			Local:          LocalStorage{Root: "data/storage"},
			S3:             S3Storage{Prefix: "sharepipe", ServerSideEncryption: "AES256"},
		},
		Uploads: UploadConfig{
			ChunkSizeBytes: 8 * 1024 * 1024, MaxFileBytes: 10 * 1024 * 1024 * 1024,
			MaxShareBytes: 20 * 1024 * 1024 * 1024, MaxFilesPerShare: 1000, SessionTTLMinutes: 1440,
		},
		Sharing: SharingConfig{
			DefaultExpiry: "168h", AllowedExpiries: []string{"1h", "24h", "168h", "720h"},
			DefaultDownloadLimit: 10, GrantTTLMinutes: 15, MetadataRetention: "720h",
		},
		Zip:     ZipConfig{AsyncThresholdBytes: 2 * 1024 * 1024 * 1024, CacheTTL: "24h"},
		Backup:  BackupConfig{Directory: "data/backups", Hour: 3, RetentionCount: 7},
		Logging: LoggingConfig{Directory: "workdir/logs", DiagnosticRetention: "168h"},
		Scanner: ScannerConfig{
			TimeoutSeconds: 300, CleanExitCodes: []int{0}, InfectedExitCodes: []int{1}, FailClosed: true,
		},
	}
}

func Load(path string) (Config, error) {
	cfg := Defaults()
	if path != "" {
		file, err := os.Open(path)
		if err != nil {
			return Config{}, fmt.Errorf("open config: %w", err)
		}
		defer file.Close()
		decoder := json.NewDecoder(file)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&cfg); err != nil {
			return Config{}, fmt.Errorf("decode config: %w", err)
		}
	}
	cfg.applyEnvironment()
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c *Config) applyEnvironment() {
	override(&c.Server.Listen, "SHAREPIPE_LISTEN")
	override(&c.Server.PublicBaseURL, "SHAREPIPE_PUBLIC_BASE_URL")
	override(&c.Database.Path, "SHAREPIPE_DATABASE_PATH")
	override(&c.Storage.Local.Root, "SHAREPIPE_LOCAL_STORAGE_ROOT")
	override(&c.Storage.DefaultBackend, "SHAREPIPE_DEFAULT_STORAGE")
	override(&c.Storage.S3.Endpoint, "SHAREPIPE_S3_ENDPOINT")
	override(&c.Storage.S3.Region, "SHAREPIPE_S3_REGION")
	override(&c.Storage.S3.Bucket, "SHAREPIPE_S3_BUCKET")
	override(&c.Storage.S3.Prefix, "SHAREPIPE_S3_PREFIX")
	c.AdminPassword = os.Getenv("SHAREPIPE_ADMIN_PASSWORD")
	c.AdminPasswordHash = os.Getenv("SHAREPIPE_ADMIN_PASSWORD_HASH")
	c.SessionSecret = os.Getenv("SHAREPIPE_SESSION_SECRET")
	c.StorageMasterKey = os.Getenv("SHAREPIPE_STORAGE_MASTER_KEY")
	c.S3AccessKeyID = os.Getenv("SHAREPIPE_S3_ACCESS_KEY_ID")
	c.S3SecretAccessKey = os.Getenv("SHAREPIPE_S3_SECRET_ACCESS_KEY")
	c.S3SessionToken = os.Getenv("SHAREPIPE_S3_SESSION_TOKEN")
	c.CloudControlToken = os.Getenv("SHAREPIPE_CLOUD_CONTROL_TOKEN")
	override(&c.Server.CloudflareAccess.TeamDomain, "SHAREPIPE_CF_ACCESS_TEAM_DOMAIN")
	override(&c.Server.CloudflareAccess.Audience, "SHAREPIPE_CF_ACCESS_AUDIENCE")
	if raw := strings.TrimSpace(os.Getenv("SHAREPIPE_ADMIN_EMAILS")); raw != "" {
		c.Admin.InitialEmails = strings.Split(raw, ",")
	}
	if raw := os.Getenv("SHAREPIPE_S3_PATH_STYLE"); raw != "" {
		if value, err := strconv.ParseBool(raw); err == nil {
			c.Storage.S3.PathStyle = value
		}
	}
}

func override(target *string, name string) {
	if value := os.Getenv(name); value != "" {
		*target = value
	}
}

func (c Config) Validate() error {
	var problems []string
	if strings.TrimSpace(c.Server.Listen) == "" {
		problems = append(problems, "server.listen is required")
	}
	base, err := url.Parse(c.Server.PublicBaseURL)
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" ||
		(base.Path != "" && base.Path != "/") || base.RawQuery != "" || base.Fragment != "" {
		problems = append(problems, "server.publicBaseURL must be an absolute http(s) URL")
	}
	for _, raw := range c.Server.TrustedProxyCIDRs {
		if _, _, err := net.ParseCIDR(raw); err != nil {
			problems = append(problems, "invalid server.trustedProxyCIDRs entry: "+raw)
		}
	}
	if c.Server.AdminAccess != "local" && c.Server.AdminAccess != "cloudflare-access" &&
		c.Server.AdminAccess != "public" {
		problems = append(problems, "server.adminAccess must be local, cloudflare-access, or public")
	}
	if c.Server.AdminAccess == "cloudflare-access" {
		team, teamErr := url.Parse(strings.TrimRight(c.Server.CloudflareAccess.TeamDomain, "/"))
		if teamErr != nil || team.Scheme != "https" || team.Host == "" || team.Path != "" || team.User != nil ||
			team.RawQuery != "" || team.Fragment != "" {
			problems = append(problems, "server.cloudflareAccess.teamDomain must be an HTTPS origin")
		}
		if strings.TrimSpace(c.Server.CloudflareAccess.Audience) == "" {
			problems = append(problems, "server.cloudflareAccess.audience is required in cloudflare-access mode")
		}
	}
	for _, email := range c.Admin.InitialEmails {
		email = strings.ToLower(strings.TrimSpace(email))
		if email == "" || !strings.Contains(email, "@") || strings.ContainsAny(email, " \t\r\n") {
			problems = append(problems, "admin.initialEmails contains an invalid email address")
		}
	}
	if c.Admin.CloudControlURL != "" {
		control, controlErr := url.Parse(strings.TrimRight(c.Admin.CloudControlURL, "/"))
		if controlErr != nil || control.Scheme != "https" || control.Host == "" || control.Path != "" ||
			control.User != nil || control.RawQuery != "" || control.Fragment != "" {
			problems = append(problems, "admin.cloudControlURL must be an HTTPS origin")
		}
		if strings.TrimSpace(c.CloudControlToken) == "" {
			problems = append(problems, "SHAREPIPE_CLOUD_CONTROL_TOKEN is required when admin.cloudControlURL is configured")
		}
	}
	if c.Database.Path == "" {
		problems = append(problems, "database.path is required")
	}
	if c.Database.BusyTimeoutMS < 1 {
		problems = append(problems, "database.busyTimeoutMs must be positive")
	}
	if c.Storage.DefaultBackend != "local" && c.Storage.DefaultBackend != "s3" {
		problems = append(problems, "storage.defaultBackend must be local or s3")
	}
	if c.Storage.Local.Root == "" {
		problems = append(problems, "storage.local.root is required")
	}
	if c.Storage.DefaultBackend == "s3" && !c.Storage.S3.Enabled {
		problems = append(problems, "S3 must be enabled when it is the default backend")
	}
	if c.Storage.S3.Enabled && (c.Storage.S3.Region == "" || c.Storage.S3.Bucket == "") {
		problems = append(problems, "S3 region and bucket are required when S3 is enabled")
	}
	if c.Storage.S3.Enabled && c.Storage.S3.Endpoint != "" {
		endpoint, endpointErr := url.Parse(c.Storage.S3.Endpoint)
		if endpointErr != nil || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.Host == "" {
			problems = append(problems, "storage.s3.endpoint must be an absolute http(s) URL")
		}
	}
	if c.Uploads.ChunkSizeBytes < 1024*1024 || c.Uploads.ChunkSizeBytes >= 100*1024*1024 {
		problems = append(problems, "uploads.chunkSizeBytes must be at least 1 MiB and below 100 MiB")
	}
	if c.Uploads.MaxFileBytes < c.Uploads.ChunkSizeBytes {
		problems = append(problems, "uploads.maxFileBytes must be at least one chunk")
	}
	if c.Uploads.MaxShareBytes < c.Uploads.MaxFileBytes {
		problems = append(problems, "uploads.maxShareBytes must be at least maxFileBytes")
	}
	if c.Uploads.MaxFilesPerShare < 1 || c.Uploads.SessionTTLMinutes < 1 {
		problems = append(problems, "upload file count and session TTL must be positive")
	}
	allowed := false
	for _, expiry := range c.Sharing.AllowedExpiries {
		if _, err := time.ParseDuration(expiry); err != nil {
			problems = append(problems, "invalid allowed expiry: "+expiry)
		}
		if expiry == c.Sharing.DefaultExpiry {
			allowed = true
		}
	}
	if !allowed {
		problems = append(problems, "sharing.defaultExpiry must be in allowedExpiries")
	}
	for name, raw := range map[string]string{
		"sharing.metadataRetention":   c.Sharing.MetadataRetention,
		"zip.cacheTTL":                c.Zip.CacheTTL,
		"logging.diagnosticRetention": c.Logging.DiagnosticRetention,
	} {
		if duration, err := time.ParseDuration(raw); err != nil || duration <= 0 {
			problems = append(problems, name+" must be a positive duration")
		}
	}
	if c.Sharing.GrantTTLMinutes < 1 || c.Backup.RetentionCount < 1 || c.Backup.Hour < 0 || c.Backup.Hour > 23 {
		problems = append(problems, "grant TTL and backup settings are invalid")
	}
	if c.Scanner.Enabled {
		if len(c.Scanner.Command) == 0 || strings.TrimSpace(c.Scanner.Command[0]) == "" {
			problems = append(problems, "scanner.command is required when scanning is enabled")
		}
		if c.Scanner.TimeoutSeconds < 1 {
			problems = append(problems, "scanner.timeoutSeconds must be positive")
		}
		if len(c.Scanner.CleanExitCodes) == 0 || len(c.Scanner.InfectedExitCodes) == 0 {
			problems = append(problems, "scanner clean and infected exit codes are required")
		}
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

func (c Config) ResolvePaths(root string) Config {
	result := c
	result.Database.Path = resolve(root, c.Database.Path)
	result.Storage.Local.Root = resolve(root, c.Storage.Local.Root)
	result.Backup.Directory = resolve(root, c.Backup.Directory)
	result.Logging.Directory = resolve(root, c.Logging.Directory)
	return result
}

func resolve(root, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(root, filepath.Clean(path))
}

func (c Config) ExpiryAllowed(raw string) (time.Duration, bool) {
	for _, item := range c.Sharing.AllowedExpiries {
		if raw == item {
			duration, err := time.ParseDuration(item)
			return duration, err == nil
		}
	}
	return 0, false
}
