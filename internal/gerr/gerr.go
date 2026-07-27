// Package gerr maps Google API errors to friendly, actionable messages.
package gerr

import (
	"errors"
	"fmt"
	"strings"

	"google.golang.org/api/googleapi"
)

// Friendly rewrites common googleapi.Error codes; other errors pass through.
func Friendly(err error) error {
	if err == nil {
		return nil
	}
	var ge *googleapi.Error
	if errors.As(err, &ge) {
		switch {
		case ge.Code == 403 && apiNotEnabled(ge):
			return fmt.Errorf("the Google Chat API is not enabled for this OAuth client's GCP project: "+
				"enable it at https://console.cloud.google.com/apis/library/chat.googleapis.com then retry: %w", err)
		case (ge.Code == 401 || ge.Code == 403) && insufficientScope(ge):
			return fmt.Errorf("missing OAuth scopes: run 'gsvc auth login' again to grant the new permissions: %w", err)
		case ge.Code == 401 || ge.Code == 403:
			return fmt.Errorf("permission denied (check auth/scopes): %w", err)
		case ge.Code == 404:
			return fmt.Errorf("not found: %w", err)
		case ge.Code == 429:
			return fmt.Errorf("rate limited, please try again shortly: %w", err)
		case ge.Code >= 500:
			return fmt.Errorf("Google server error, please try again: %w", err)
		}
	}
	return err
}

// insufficientScope reports whether a 401/403 was caused by the access token
// lacking a scope rather than by the account lacking permission.
func insufficientScope(ge *googleapi.Error) bool {
	if strings.Contains(ge.Message, "insufficient authentication scopes") {
		return true
	}
	for _, e := range ge.Errors {
		if e.Reason == "ACCESS_TOKEN_SCOPE_INSUFFICIENT" || e.Reason == "insufficientPermissions" {
			return true
		}
	}
	return false
}

// apiNotEnabled reports whether a 403 means the API itself is switched off in
// the OAuth client's GCP project.
func apiNotEnabled(ge *googleapi.Error) bool {
	return strings.Contains(ge.Message, "has not been used in project") ||
		strings.Contains(ge.Message, "SERVICE_DISABLED")
}
