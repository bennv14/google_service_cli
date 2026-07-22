package gerr

import (
	"errors"
	"testing"

	"google.golang.org/api/googleapi"
)

func TestFriendly(t *testing.T) {
	tests := []struct {
		name    string
		input   error
		wantErr string
	}{
		{
			name:    "nil error",
			input:   nil,
			wantErr: "",
		},
		{
			name:    "non googleapi error",
			input:   errors.New("custom error"),
			wantErr: "custom error",
		},
		{
			name:    "401 unauthorized",
			input:   &googleapi.Error{Code: 401, Message: "Unauthorized"},
			wantErr: "permission denied (check auth/scopes): googleapi: Error 401: Unauthorized",
		},
		{
			name:    "403 forbidden",
			input:   &googleapi.Error{Code: 403, Message: "Forbidden"},
			wantErr: "permission denied (check auth/scopes): googleapi: Error 403: Forbidden",
		},
		{
			name:    "404 not found",
			input:   &googleapi.Error{Code: 404, Message: "File not found"},
			wantErr: "not found: googleapi: Error 404: File not found",
		},
		{
			name:    "429 rate limit",
			input:   &googleapi.Error{Code: 429, Message: "User Rate Limit Exceeded"},
			wantErr: "rate limited, please try again shortly: googleapi: Error 429: User Rate Limit Exceeded",
		},
		{
			name:    "500 server error",
			input:   &googleapi.Error{Code: 500, Message: "Internal Error"},
			wantErr: "Google server error, please try again: googleapi: Error 500: Internal Error",
		},
		{
			name:    "400 bad request unmapped",
			input:   &googleapi.Error{Code: 400, Message: "Bad Request"},
			wantErr: "googleapi: Error 400: Bad Request",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Friendly(tt.input)
			if tt.wantErr == "" {
				if got != nil {
					t.Errorf("Friendly() = %v, want nil", got)
				}
			} else {
				if got == nil || got.Error() != tt.wantErr {
					t.Errorf("Friendly() = %v, want %v", got, tt.wantErr)
				}
			}
		})
	}
}
