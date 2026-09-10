package oauth

import (
	"errors"
)

// maxErrorDescriptionLen bounds how much of IAM's error_description ends up in
// --format json output, so a misbehaving or overly verbose server response
// cannot balloon into an unreadable message or a malformed line of output.
const maxErrorDescriptionLen = 500

// ClassifyAuthorizationError turns a Login error into a stable, user-facing
// message for the --format json output.
//
// Only access_denied gets a fixed sentence, matching the common "user clicked
// cancel" case. Any other OAuth error code carries IAM's own description, since
// the set of codes is defined server-side and the CLI should not guess at
// wording for codes it does not recognise. A callback timeout gets its own
// fixed sentence. Anything else (network failures, STS errors, a nonce
// mismatch, ...) is not IAM-facing text and must not leak implementation
// details into a machine-readable field, so it collapses to one generic
// sentence; the original error is still available to callers that log it.
func ClassifyAuthorizationError(err error) string {
	var authErr *AuthorizationError
	if errors.As(err, &authErr) {
		if authErr.Code == "access_denied" {
			return "Authorization was denied by the user."
		}
		desc := truncateDescription(authErr.Description)
		if desc != "" {
			return "Authorization failed: " + authErr.Code + ": " + desc
		}
		return "Authorization failed: " + authErr.Code
	}
	if errors.Is(err, ErrCallbackTimeout) {
		return "Authorization timeout."
	}
	return "Authorization failed."
}

func truncateDescription(s string) string {
	if len(s) <= maxErrorDescriptionLen {
		return s
	}
	return s[:maxErrorDescriptionLen] + "..."
}
