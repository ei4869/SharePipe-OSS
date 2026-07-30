package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ei4869/SharePipe-OSS/internal/core"
)

type cloudAdminClient struct {
	baseURL string
	token   string
	client  *http.Client
}

func newCloudAdminClient(baseURL, token string) *cloudAdminClient {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" || strings.TrimSpace(token) == "" {
		return nil
	}
	return &cloudAdminClient{
		baseURL: baseURL,
		token:   token,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (s *Service) CloudAdministratorControlConfigured() bool { return s.cloudAdmin != nil }

func (s *Service) CloudAdministratorSecurity(ctx context.Context) (AdministratorSecurity, error) {
	var result AdministratorSecurity
	if err := s.cloudAdmin.request(ctx, http.MethodGet, "/api/v1/control/security", nil, &result); err != nil {
		return AdministratorSecurity{}, err
	}
	return result, nil
}

func (s *Service) CloudAddAdministratorEmail(ctx context.Context, email string) (string, error) {
	var result struct {
		Email string `json:"email"`
	}
	if err := s.cloudAdmin.request(ctx, http.MethodPost, "/api/v1/control/security/emails", map[string]string{"email": email}, &result); err != nil {
		return "", err
	}
	return result.Email, nil
}

func (s *Service) CloudRemoveAdministratorEmail(ctx context.Context, email string) error {
	return s.cloudAdmin.request(ctx, http.MethodDelete, "/api/v1/control/security/emails/"+url.PathEscape(email), nil, nil)
}

func (s *Service) CloudChangeAdministratorPassword(ctx context.Context, newPassword string) error {
	characters := utf8.RuneCountInString(newPassword)
	if characters < 12 || characters > 256 {
		return fmt.Errorf("%w: administrator password must contain 12-256 characters", core.ErrValidation)
	}
	return s.cloudAdmin.request(ctx, http.MethodPut, "/api/v1/control/security/password", map[string]string{"newPassword": newPassword}, nil)
}

func (s *Service) CloudTrustedDevices(ctx context.Context) ([]core.TrustedDevice, error) {
	var result struct {
		Items []core.TrustedDevice `json:"items"`
	}
	if err := s.cloudAdmin.request(ctx, http.MethodGet, "/api/v1/control/trusted-devices", nil, &result); err != nil {
		return nil, err
	}
	return result.Items, nil
}

func (s *Service) CloudRenameTrustedDevice(ctx context.Context, id, name string) (core.TrustedDevice, error) {
	var result core.TrustedDevice
	err := s.cloudAdmin.request(ctx, http.MethodPatch, "/api/v1/control/trusted-devices/"+url.PathEscape(id), map[string]string{"name": name}, &result)
	return result, err
}

func (s *Service) CloudRevokeTrustedDevice(ctx context.Context, id string) error {
	return s.cloudAdmin.request(ctx, http.MethodDelete, "/api/v1/control/trusted-devices/"+url.PathEscape(id), nil, nil)
}

func (s *Service) CloudTrustedDeviceBootstrap(ctx context.Context, name string) (string, time.Time, error) {
	var result struct {
		URL       string    `json:"url"`
		ExpiresAt time.Time `json:"expiresAt"`
	}
	err := s.cloudAdmin.request(ctx, http.MethodPost, "/api/v1/control/trusted-devices/bootstrap", map[string]string{"name": name}, &result)
	return result.URL, result.ExpiresAt, err
}

func (c *cloudAdminClient) request(ctx context.Context, method, path string, input, output any) error {
	if c == nil {
		return errors.New("cloud administrator control is not configured")
	}
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Accept", "application/json")
	if input != nil {
		request.Header.Set("Content-Type", "application/json; charset=utf-8")
	}
	response, err := c.client.Do(request)
	if err != nil {
		return fmt.Errorf("cloud administrator control request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64*1024))
		return cloudAdminStatusError(response.StatusCode)
	}
	if output == nil || response.StatusCode == http.StatusNoContent {
		_, _ = io.Copy(io.Discard, response.Body)
		return nil
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 256*1024))
	if err := decoder.Decode(output); err != nil {
		return fmt.Errorf("decode cloud administrator control response: %w", err)
	}
	return nil
}

func cloudAdminStatusError(status int) error {
	switch status {
	case http.StatusBadRequest:
		return core.ErrValidation
	case http.StatusUnauthorized:
		return core.ErrUnauthorized
	case http.StatusForbidden:
		return core.ErrForbidden
	case http.StatusNotFound:
		return core.ErrNotFound
	case http.StatusConflict:
		return core.ErrConflict
	default:
		return fmt.Errorf("cloud administrator control returned HTTP %d", status)
	}
}
