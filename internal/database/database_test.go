package database

import (
	"context"
	"database/sql"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

func TestOpenMigratesAndEnablesWAL(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "sharepipe.db")
	db, err := Open(ctx, path, 1000)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var mode string
	if err := db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if mode != "wal" {
		t.Fatalf("expected WAL mode, got %s", mode)
	}
	var tableCount int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='shares'").Scan(&tableCount); err != nil {
		t.Fatal(err)
	}
	if tableCount != 1 {
		t.Fatal("shares table was not migrated")
	}

	backup := filepath.Join(t.TempDir(), "backup.db")
	if err := db.Backup(ctx, backup); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(backup); err != nil || info.Size() == 0 {
		t.Fatalf("backup was not created: %v", err)
	}
}

func TestReliabilityMigrationUpgradesExistingDatabaseAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "upgrade.db")
	dsn, err := sqliteDSN(path, 1000)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if _, err := raw.ExecContext(ctx, `CREATE TABLE schema_migrations(version TEXT PRIMARY KEY,applied_at INTEGER NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	files, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	for _, name := range files {
		if filepath.Base(name) >= "005_reliability_controls.sql" {
			continue
		}
		body, readErr := migrations.ReadFile(name)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if _, err := raw.ExecContext(ctx, string(body)); err != nil {
			t.Fatalf("apply legacy migration %s: %v", name, err)
		}
		if _, err := raw.ExecContext(ctx, "INSERT INTO schema_migrations(version,applied_at) VALUES(?,?)", filepath.Base(name), time.Now().Unix()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := raw.ExecContext(ctx, `INSERT INTO shares(
		id,public_token_hash,manage_token_hash,name,description,storage_backend,status,expires_at,
		download_count,file_count,total_bytes,created_at,updated_at)
		VALUES('legacy-upload','public','manage','legacy','','local','uploading',?,0,1,4,?,?)`,
		time.Now().Add(time.Hour).Unix(), time.Now().Unix(), time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	db := &Database{DB: raw, path: path}
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("second migration run failed: %v", err)
	}
	var passwordVersion int
	var taskStatus string
	if err := raw.QueryRowContext(ctx, `SELECT s.password_version,t.status FROM shares s
		JOIN upload_tasks t ON t.share_id=s.id WHERE s.id='legacy-upload'`).Scan(&passwordVersion, &taskStatus); err != nil {
		t.Fatal(err)
	}
	if passwordVersion != 1 || taskStatus != "uploading" {
		t.Fatalf("upgraded row = password_version %d, task %s", passwordVersion, taskStatus)
	}
	var applied int
	if err := raw.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations WHERE version='005_reliability_controls.sql'").Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied != 1 {
		t.Fatalf("reliability migration applied %d times", applied)
	}
	if err := raw.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations WHERE version='006_upload_state_machine.sql'").Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied != 1 {
		t.Fatalf("upload state migration applied %d times", applied)
	}
}

func TestOpenAppliesConnectionScopedPragmasToEntirePool(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "pool.db"), 2345)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	connections := make([]*sql.Conn, 0, 4)
	defer func() {
		for _, connection := range connections {
			connection.Close()
		}
	}()
	for range 4 {
		connection, err := db.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		connections = append(connections, connection)
	}
	for index, connection := range connections {
		var foreignKeys, busyTimeout int
		if err := connection.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
			t.Fatalf("connection %d foreign_keys: %v", index, err)
		}
		if err := connection.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
			t.Fatalf("connection %d busy_timeout: %v", index, err)
		}
		if foreignKeys != 1 || busyTimeout != 2345 {
			t.Fatalf("connection %d pragmas: foreign_keys=%d busy_timeout=%d", index, foreignKeys, busyTimeout)
		}
	}
}
