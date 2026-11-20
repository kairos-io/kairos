package client

import (
	"context"
	"net/http"
)

// SettingsService groups instance-level settings endpoints.
type SettingsService struct{ c *Client }

// GetRegistrationToken reads the current registration token.
func (s *SettingsService) GetRegistrationToken(ctx context.Context) (string, error) {
	var out RegistrationTokenResponse
	if err := s.c.do(ctx, http.MethodGet, "/api/v1/settings/registration-token", nil, nil, &out); err != nil {
		return "", err
	}
	return out.RegistrationToken, nil
}

// RotateRegistrationToken generates and returns a new registration
// token. Already-registered nodes are unaffected because they
// authenticate with their api key, not the registration token.
func (s *SettingsService) RotateRegistrationToken(ctx context.Context) (string, error) {
	var out RegistrationTokenResponse
	if err := s.c.do(ctx, http.MethodPost, "/api/v1/settings/registration-token/rotate", nil, nil, &out); err != nil {
		return "", err
	}
	return out.RegistrationToken, nil
}
