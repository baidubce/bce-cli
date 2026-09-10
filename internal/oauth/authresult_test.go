package oauth

import (
	"fmt"
	"strings"
	"testing"
)

func TestClassifyAuthorizationError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "access denied",
			err:  &AuthorizationError{Code: "access_denied", Description: "user cancelled"},
			want: "Authorization was denied by the user.",
		},
		{
			name: "access denied without description",
			err:  &AuthorizationError{Code: "access_denied"},
			want: "Authorization was denied by the user.",
		},
		{
			name: "other oauth error with description",
			err:  &AuthorizationError{Code: "server_error", Description: "temporarily unavailable"},
			want: "Authorization failed: server_error: temporarily unavailable",
		},
		{
			name: "other oauth error without description",
			err:  &AuthorizationError{Code: "server_error"},
			want: "Authorization failed: server_error",
		},
		{
			name: "callback timeout",
			err:  fmt.Errorf("%w after 5m0s", ErrCallbackTimeout),
			want: "Authorization timeout.",
		},
		{
			name: "unrelated error falls back to a generic sentence, not the raw message",
			err:  fmt.Errorf("call token endpoint: connection refused"),
			want: "Authorization failed.",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyAuthorizationError(tc.err); got != tc.want {
				t.Errorf("ClassifyAuthorizationError(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}

func TestClassifyAuthorizationErrorTruncatesLongDescriptions(t *testing.T) {
	longDesc := strings.Repeat("x", maxErrorDescriptionLen+100)
	got := ClassifyAuthorizationError(&AuthorizationError{Code: "server_error", Description: longDesc})
	if len(got) > len("Authorization failed: server_error: ")+maxErrorDescriptionLen+3 {
		t.Errorf("message not truncated, length = %d", len(got))
	}
	if !strings.HasSuffix(got, "...") {
		t.Errorf("truncated message must end with '...', got %q", got[len(got)-20:])
	}
}
