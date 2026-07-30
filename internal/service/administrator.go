package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/mail"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ei4869/SharePipe-OSS/internal/core"
	"github.com/ei4869/SharePipe-OSS/internal/security"
)

type AdministratorSecurity struct {
	Emails             []string `json:"emails"`
	PasswordConfigured bool     `json:"passwordConfigured"`
	PasswordVersion    int      `json:"passwordVersion"`
}

func (s *Service) initializeAdministrator(ctx context.Context, bootstrapHash string) error {
	normalizedEmails := make([]string, 0, len(s.cfg.Admin.InitialEmails))
	for _, raw := range s.cfg.Admin.InitialEmails {
		email, err := normalizeEmail(raw)
		if err != nil {
			return err
		}
		normalizedEmails = append(normalizedEmails, email)
	}
	var emailCount int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM admin_emails").Scan(&emailCount); err != nil {
		return err
	}
	if emailCount == 0 {
		for _, email := range normalizedEmails {
			if _, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO admin_emails(email, created_at)
				VALUES(?, ?)`, email, s.now().Unix()); err != nil {
				return err
			}
		}
	}
	var hash string
	err := s.db.QueryRowContext(ctx, "SELECT value FROM settings WHERE key='admin_password_hash'").Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) && bootstrapHash != "" {
		if _, verifyErr := security.VerifyPassword(bootstrapHash, "verification-only"); verifyErr != nil {
			return fmt.Errorf("invalid bootstrap administrator hash: %w", verifyErr)
		}
		_, err = s.db.ExecContext(ctx, `INSERT INTO settings(key,value,updated_at)
			VALUES('admin_password_hash',?,?)`, bootstrapHash, s.now().Unix())
		hash = bootstrapHash
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	s.adminPasswordHash.Store(hash)
	return nil
}

func (s *Service) AdministratorSecurity(ctx context.Context) (AdministratorSecurity, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT email FROM admin_emails ORDER BY email")
	if err != nil {
		return AdministratorSecurity{}, err
	}
	defer rows.Close()
	result := AdministratorSecurity{Emails: make([]string, 0), PasswordVersion: s.passwordVersion(ctx)}
	for rows.Next() {
		var email string
		if err := rows.Scan(&email); err != nil {
			return AdministratorSecurity{}, err
		}
		result.Emails = append(result.Emails, email)
	}
	if err := rows.Err(); err != nil {
		return AdministratorSecurity{}, err
	}
	hash, _ := s.adminPasswordHash.Load().(string)
	result.PasswordConfigured = hash != ""
	return result, nil
}

func (s *Service) AdminEmailAllowed(ctx context.Context, raw string) bool {
	email, err := normalizeEmail(raw)
	if err != nil {
		return false
	}
	var exists int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM admin_emails WHERE email=?", email).Scan(&exists); err != nil {
		return false
	}
	return exists == 1
}

func (s *Service) AddAdminEmail(ctx context.Context, raw string) (string, error) {
	email, err := normalizeEmail(raw)
	if err != nil {
		return "", fmt.Errorf("%w: %v", core.ErrValidation, err)
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO admin_emails(email, created_at) VALUES(?, ?)`, email, s.now().Unix())
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return "", fmt.Errorf("%w: administrator email already exists", core.ErrConflict)
		}
		return "", err
	}
	return email, nil
}

func (s *Service) RemoveAdminEmail(ctx context.Context, raw string) error {
	email, err := normalizeEmail(raw)
	if err != nil {
		return fmt.Errorf("%w: %v", core.ErrValidation, err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var count int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM admin_emails").Scan(&count); err != nil {
		return err
	}
	if count <= 1 {
		return fmt.Errorf("%w: at least one administrator email must remain", core.ErrConflict)
	}
	result, err := tx.ExecContext(ctx, "DELETE FROM admin_emails WHERE email=?", email)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return core.ErrNotFound
	}
	return tx.Commit()
}

func (s *Service) ChangeAdminPassword(ctx context.Context, currentPassword, newPassword string, requireCurrent bool) error {
	characters := utf8.RuneCountInString(newPassword)
	if characters < 12 || characters > 256 {
		return fmt.Errorf("%w: administrator password must contain 12-256 characters", core.ErrValidation)
	}
	if requireCurrent && !s.AuthenticateAdmin(currentPassword) {
		return core.ErrUnauthorized
	}
	hash, err := security.HashPassword(newPassword)
	if err != nil {
		return fmt.Errorf("%w: %v", core.ErrValidation, err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var rawVersion string
	if err := tx.QueryRowContext(ctx, "SELECT value FROM settings WHERE key='admin_password_version'").Scan(&rawVersion); err != nil {
		return err
	}
	version, _ := strconv.Atoi(rawVersion)
	if version < 1 {
		version = 1
	}
	now := s.now().Unix()
	if _, err := tx.ExecContext(ctx, `INSERT INTO settings(key,value,updated_at)
		VALUES('admin_password_hash',?,?) ON CONFLICT(key) DO UPDATE SET
		value=excluded.value, updated_at=excluded.updated_at`, hash, now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE settings SET value=?, updated_at=?
		WHERE key='admin_password_version'`, strconv.Itoa(version+1), now); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.adminPasswordHash.Store(hash)
	return nil
}

func normalizeEmail(raw string) (string, error) {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if len(raw) < 3 || len(raw) > 254 || strings.ContainsAny(raw, "\r\n\t ") {
		return "", errors.New("invalid email address")
	}
	parsed, err := mail.ParseAddress(raw)
	if err != nil || strings.ToLower(parsed.Address) != raw {
		return "", errors.New("invalid email address")
	}
	return raw, nil
}

func (s *Service) AdministratorSessionTTL() time.Duration { return 12 * time.Hour }
