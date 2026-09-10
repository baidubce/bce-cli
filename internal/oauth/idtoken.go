package oauth

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// IDTokenClaims are the ID Token claims the CLI reads locally.
//
// These claims are NOT verified here: signature, issuer, audience and identity
// checks are performed by STS against the IAM JWKS. The CLI decodes them only
// to display the logged-in identity and to decide when to refresh, never to
// make an authorization decision.
type IDTokenClaims struct {
	Subject       string `json:"sub"`
	AccountID     string `json:"account_id"`
	PrincipalID   string `json:"principal_id"`
	PrincipalType string `json:"principal_type"`
	Username      string `json:"username"`
	Scope         string `json:"scope"`
	Nonce         string `json:"nonce"`
	Expiry        int64  `json:"exp"`
	IssuedAt      int64  `json:"iat"`
}

// ExpiresAt returns the token expiry as a timestamp, or the zero time when the
// exp claim is absent.
func (c *IDTokenClaims) ExpiresAt() time.Time {
	if c == nil || c.Expiry <= 0 {
		return time.Time{}
	}
	return time.Unix(c.Expiry, 0)
}

// DecodeIDToken parses the unverified payload segment of a JWT.
func DecodeIDToken(idToken string) (*IDTokenClaims, error) {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("malformed id_token: expected 3 segments, got %d", len(parts))
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("decode id_token payload: %w", err)
	}
	var claims IDTokenClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("parse id_token payload: %w", err)
	}
	return &claims, nil
}
