// Package gerr maps Google API errors to friendly, actionable messages.
package gerr

import (
	"errors"
	"fmt"

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
