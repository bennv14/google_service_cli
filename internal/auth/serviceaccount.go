package auth

import (
	"context"
	"errors"
	"fmt"
	"os"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/bennv/google_service_cli/internal/config"
)

type serviceAccountProvider struct{ keyData []byte }

func newServiceAccountProvider(p config.Profile) (*serviceAccountProvider, error) {
	if p.KeyPath == "" {
		return nil, errors.New("service_account profile requires key_path")
	}
	b, err := os.ReadFile(expandPath(p.KeyPath))
	if err != nil {
		return nil, fmt.Errorf("read service account key: %w", err)
	}
	return &serviceAccountProvider{keyData: b}, nil
}

func (s *serviceAccountProvider) Kind() string { return "service_account" }

func (s *serviceAccountProvider) TokenSource(ctx context.Context, scopes ...string) (oauth2.TokenSource, error) {
	cfg, err := google.JWTConfigFromJSON(s.keyData, scopes...)
	if err != nil {
		return nil, fmt.Errorf("parse service account key: %w", err)
	}
	return cfg.TokenSource(ctx), nil
}
