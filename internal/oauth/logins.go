package oauth

import "time"

// ActiveLogin reports the token set for profile if it is still usable, or nil
// if there is none.
//
// A login counts as active while its Refresh Token is valid, or while its ID
// Token can still be exchanged. Leftover state whose Refresh Token has expired
// is treated as absent: reporting it as active would leave the user unable to
// log in without first running `bce auth logout`. Missing or unreadable state
// is likewise treated as absent, so callers proceed with a fresh login rather
// than failing.
func ActiveLogin(profile string) *TokenSet {
	ts, err := LoadTokens(profile)
	if err != nil {
		return nil
	}
	if !loginActive(ts) {
		return nil
	}
	return ts
}

// loginActive reports whether a token set can still produce credentials.
func loginActive(ts *TokenSet) bool {
	if ts.RefreshToken != "" {
		if ts.RefreshTokenExpiresAt.IsZero() || time.Now().Before(ts.RefreshTokenExpiresAt) {
			return true
		}
	}
	return idTokenUsable(ts)
}
