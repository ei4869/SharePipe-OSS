package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ei4869/SharePipe-OSS/internal/config"
	"github.com/ei4869/SharePipe-OSS/internal/database"
	"github.com/ei4869/SharePipe-OSS/internal/httpapi"
	"github.com/ei4869/SharePipe-OSS/internal/scanner"
	"github.com/ei4869/SharePipe-OSS/internal/security"
	"github.com/ei4869/SharePipe-OSS/internal/service"
	"golang.org/x/term"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

//go:embed webdist
var embeddedWeb embed.FS

func main() {
	if len(os.Args) > 1 && os.Args[1] == "hash-password" {
		if err := hashPasswordCommand(); err != nil {
			log.Fatal(err)
		}
		return
	}
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	var configPath string
	var showVersion bool
	flag.StringVar(&configPath, "config", "config/sharepipe.json", "path to the SharePipe JSON configuration")
	flag.BoolVar(&showVersion, "version", false, "print build version")
	flag.Parse()
	if showVersion {
		fmt.Printf("SharePipe %s (%s, %s)\n", version, commit, date)
		return nil
	}
	if _, err := os.Stat(configPath); errors.Is(err, os.ErrNotExist) {
		configPath = ""
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	projectRoot, err := os.Getwd()
	if err != nil {
		return err
	}
	cfg = cfg.ResolvePaths(projectRoot)
	if err := ensureRuntimeDirectories(cfg); err != nil {
		return err
	}

	ctx := context.Background()
	db, err := database.Open(ctx, cfg.Database.Path, cfg.Database.BusyTimeoutMS)
	if err != nil {
		return err
	}
	defer db.Close()

	secret, err := loadOrCreateSecret(filepath.Join(filepath.Dir(cfg.Database.Path), "session.key"), cfg.SessionSecret, "session secret")
	if err != nil {
		return err
	}
	storageMasterKey, err := loadOrCreateSecret(filepath.Join(filepath.Dir(cfg.Database.Path), "storage.key"), cfg.StorageMasterKey, "storage master key")
	if err != nil {
		return err
	}
	if len(storageMasterKey) != 32 {
		return errors.New("SHAREPIPE_STORAGE_MASTER_KEY must decode to exactly 32 bytes")
	}
	signer, err := security.NewSigner(secret)
	if err != nil {
		return err
	}
	adminHash, err := administratorHash(cfg)
	if err != nil {
		return err
	}
	contentScanner, err := configuredScanner(cfg)
	if err != nil {
		return err
	}
	auditSeed := sha256.Sum256(append(append([]byte(nil), secret...), []byte("sharepipe-audit")...))
	svc, err := service.New(service.Options{
		Database: db, Config: cfg, Signer: signer, AdminPasswordHash: adminHash,
		StorageMasterKey: storageMasterKey,
		StagingRoot:      filepath.Join(projectRoot, "workdir", "intermediate", "uploads"), AuditSalt: auditSeed[:],
		Scanner: contentScanner,
	})
	if err != nil {
		return err
	}
	static, err := webFS()
	if err != nil {
		return err
	}
	handler := httpapi.New(svc, static)
	server := &http.Server{
		Addr: cfg.Server.Listen, Handler: handler, ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout: 2 * time.Minute, MaxHeaderBytes: 1 << 20,
	}

	workerContext, cancelWorkers := context.WithCancel(context.Background())
	defer cancelWorkers()
	go svc.RunBackground(workerContext)

	errorsChannel := make(chan error, 1)
	go func() {
		log.Printf("SharePipe %s listening on %s", version, cfg.Server.Listen)
		errorsChannel <- server.ListenAndServe()
	}()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	select {
	case signal := <-signals:
		log.Printf("received %s, shutting down", signal)
	case err := <-errorsChannel:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}
	cancelWorkers()
	shutdownContext, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownContext); err != nil {
		return err
	}
	return db.Checkpoint(shutdownContext)
}

func configuredScanner(cfg config.Config) (scanner.Scanner, error) {
	if !cfg.Scanner.Enabled {
		return scanner.Noop{}, nil
	}
	return scanner.NewCommand(scanner.CommandOptions{
		Command:           cfg.Scanner.Command,
		Timeout:           time.Duration(cfg.Scanner.TimeoutSeconds) * time.Second,
		CleanExitCodes:    cfg.Scanner.CleanExitCodes,
		InfectedExitCodes: cfg.Scanner.InfectedExitCodes,
	})
}

func ensureRuntimeDirectories(cfg config.Config) error {
	for _, directory := range []string{
		filepath.Dir(cfg.Database.Path), cfg.Storage.Local.Root, cfg.Backup.Directory, cfg.Logging.Directory,
	} {
		if err := os.MkdirAll(directory, 0o750); err != nil {
			return fmt.Errorf("create runtime directory %s: %w", directory, err)
		}
	}
	return nil
}

func administratorHash(cfg config.Config) (string, error) {
	if cfg.AdminPasswordHash != "" {
		if _, err := security.VerifyPassword(cfg.AdminPasswordHash, "verification-only"); err != nil {
			return "", fmt.Errorf("SHAREPIPE_ADMIN_PASSWORD_HASH is invalid: %w", err)
		}
		return cfg.AdminPasswordHash, nil
	}
	if cfg.AdminPassword == "" {
		return "", nil
	}
	return security.HashPassword(cfg.AdminPassword)
}

func loadOrCreateSecret(path, configured, label string) ([]byte, error) {
	if configured != "" {
		if decoded, err := base64.RawURLEncoding.DecodeString(configured); err == nil && len(decoded) >= 32 {
			return decoded, nil
		}
		if decoded, err := base64.URLEncoding.DecodeString(configured); err == nil && len(decoded) >= 32 {
			return decoded, nil
		}
		if decoded, err := base64.RawStdEncoding.DecodeString(configured); err == nil && len(decoded) >= 32 {
			return decoded, nil
		}
		if decoded, err := base64.StdEncoding.DecodeString(configured); err == nil && len(decoded) >= 32 {
			return decoded, nil
		}
		if len(configured) >= 32 {
			return []byte(configured), nil
		}
		return nil, fmt.Errorf("%s must contain at least 32 bytes", label)
	}
	if existing, err := os.ReadFile(path); err == nil {
		if len(existing) < 32 {
			return nil, fmt.Errorf("stored %s is too short", label)
		}
		return existing, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, err
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return os.ReadFile(path)
	}
	if err != nil {
		return nil, err
	}
	if _, err := file.Write(secret); err != nil {
		file.Close()
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	return secret, nil
}

func webFS() (fs.FS, error) {
	if application, err := fs.Sub(embeddedWeb, "webdist/app"); err == nil {
		if _, statErr := fs.Stat(application, "index.html"); statErr == nil {
			return application, nil
		}
	}
	return fs.Sub(embeddedWeb, "webdist/placeholder")
}

func hashPasswordCommand() error {
	password := os.Getenv("SHAREPIPE_PASSWORD_TO_HASH")
	if password == "" {
		fmt.Fprint(os.Stderr, "Password: ")
		first, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return err
		}
		fmt.Fprint(os.Stderr, "Repeat password: ")
		second, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return err
		}
		if string(first) != string(second) {
			return errors.New("passwords do not match")
		}
		password = string(first)
	}
	hash, err := security.HashPassword(strings.TrimRight(password, "\r\n"))
	if err != nil {
		return err
	}
	fmt.Println(hash)
	return nil
}
