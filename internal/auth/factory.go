package auth

import (
	"fmt"

	"github.com/bennv14/google_service_cli/internal/config"
)

// NewProvider selects a provider based on the profile's AuthType.
func NewProvider(p config.Profile, tokens TokenStore) (Provider, error) {
	switch p.AuthType {
	case "service_account":
		return newServiceAccountProvider(p)
	case "oauth", "":
		return newOAuthProvider(p, tokens)
	default:
		return nil, fmt.Errorf("unknown auth_type %q", p.AuthType)
	}
}
