package database

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Database struct {
	*sql.DB
	path string
}

func Open(ctx context.Context, path string, busyTimeoutMS int) (*Database, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}
	dsn, err := sqliteDSN(path, busyTimeoutMS)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(30 * time.Minute)
	instance := &Database{DB: db, path: path}
	if err := instance.configure(ctx); err != nil {
		db.Close()
		return nil, err
	}
	if err := instance.Migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return instance, nil
}

func (d *Database) configure(ctx context.Context) error {
	pragmas := []string{
		"PRAGMA journal_mode = WAL",
	}
	for _, statement := range pragmas {
		if _, err := d.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("configure sqlite with %q: %w", statement, err)
		}
	}
	if err := d.PingContext(ctx); err != nil {
		return fmt.Errorf("ping sqlite: %w", err)
	}
	return nil
}

func sqliteDSN(path string, busyTimeoutMS int) (string, error) {
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve sqlite path: %w", err)
	}
	urlPath := filepath.ToSlash(absolutePath)
	if filepath.VolumeName(absolutePath) != "" && !strings.HasPrefix(urlPath, "/") {
		urlPath = "/" + urlPath
	}
	dsnURL := url.URL{Scheme: "file", Path: urlPath}
	query := url.Values{}
	// Connection-scoped PRAGMAs must be part of the DSN so modernc/sqlite applies
	// them to every connection opened by database/sql's pool.
	query.Add("_pragma", "foreign_keys(1)")
	query.Add("_pragma", fmt.Sprintf("busy_timeout(%d)", busyTimeoutMS))
	query.Add("_pragma", "synchronous(NORMAL)")
	dsnURL.RawQuery = query.Encode()
	return dsnURL.String(), nil
}

func (d *Database) Migrate(ctx context.Context) error {
	if _, err := d.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY,
		applied_at INTEGER NOT NULL
	)`); err != nil {
		return fmt.Errorf("create migration table: %w", err)
	}
	files, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return fmt.Errorf("list migrations: %w", err)
	}
	sort.Strings(files)
	for _, name := range files {
		version := filepath.Base(name)
		var exists int
		if err := d.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations WHERE version = ?", version).Scan(&exists); err != nil {
			return fmt.Errorf("check migration %s: %w", version, err)
		}
		if exists > 0 {
			continue
		}
		sqlBytes, err := migrations.ReadFile(name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", version, err)
		}
		tx, err := d.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", version, err)
		}
		if _, err := tx.ExecContext(ctx, string(sqlBytes)); err != nil {
			tx.Rollback()
			return fmt.Errorf("apply migration %s: %w", version, err)
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations(version, applied_at) VALUES(?, ?)", version, time.Now().Unix()); err != nil {
			tx.Rollback()
			return fmt.Errorf("record migration %s: %w", version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", version, err)
		}
	}
	return nil
}

func (d *Database) Checkpoint(ctx context.Context) error {
	_, err := d.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)")
	return err
}

func (d *Database) Backup(ctx context.Context, destination string) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o750); err != nil {
		return fmt.Errorf("create backup directory: %w", err)
	}
	if _, err := os.Stat(destination); err == nil {
		return fmt.Errorf("backup destination already exists: %s", destination)
	}
	if _, err := d.ExecContext(ctx, "VACUUM INTO ?", destination); err != nil {
		return fmt.Errorf("create sqlite backup: %w", err)
	}
	return nil
}

func (d *Database) Path() string {
	return d.path
}
