package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// maxResponseBytes caps how much of a token response is read into memory.
const maxResponseBytes = 1 << 20

// Client talks to the IAM OAuth token endpoint.
type Client struct {
	HTTP      *http.Client
	Endpoints Endpoints
}

// TokenResponse is the payload returned by both the authorization_code and
// refresh_token grants.
type TokenResponse struct {
	AccessToken      string `json:"access_token"`
	TokenType        string `json:"token_type"`
	ExpiresIn        int    `json:"expires_in"`
	RefreshToken     string `json:"refresh_token"`
	RefreshExpiresIn int    `json:"refresh_expires_in"`
	Scope            string `json:"scope"`
	IDToken          string `json:"id_token"`
}

// Error is an OAuth 2.0 error response. Callers check Code for "invalid_grant"
// to decide whether the local login state must be discarded.
type Error struct {
	Code        string `json:"error"`
	Description string `json:"error_description"`
	Status      int    `json:"-"`
}

func (e *Error) Error() string {
	switch {
	case e.Code != "" && e.Description != "":
		return fmt.Sprintf("%s: %s", e.Code, e.Description)
	case e.Code != "":
		return e.Code
	default:
		return fmt.Sprintf("token endpoint returned HTTP %d", e.Status)
	}
}

// IsInvalidGrant reports whether err is an OAuth invalid_grant error, which
// means the authorization code or refresh token is no longer usable.
func IsInvalidGrant(err error) bool {
	var oe *Error
	if errors.As(err, &oe) {
		return oe.Code == "invalid_grant"
	}
	return false
}

// AuthorizationURL builds the URL the system browser must open.
// The user's credentials are only ever entered in that browser; the CLI never
// collects passwords, SMS codes or MFA input itself.
func (c *Client) AuthorizationURL(redirectURI, state, nonce, codeChallenge string) string {
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", ClientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("scope", Scope)
	q.Set("state", state)
	q.Set("code_challenge", codeChallenge)
	q.Set("code_challenge_method", "S256")
	if nonce != "" {
		q.Set("nonce", nonce)
	}
	sep := "?"
	if strings.Contains(c.Endpoints.AuthorizationEndpoint, "?") {
		sep = "&"
	}
	return c.Endpoints.AuthorizationEndpoint + sep + q.Encode()
}

// ExchangeCode swaps an authorization code for tokens using the PKCE verifier.
func (c *Client) ExchangeCode(ctx context.Context, code, redirectURI, codeVerifier string) (*TokenResponse, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", ClientID)
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("code_verifier", codeVerifier)
	return c.postToken(ctx, form)
}

// Refresh exchanges a refresh token for a new token set. IAM rotates refresh
// tokens, so the returned refresh token must replace the old one atomically and
// the old one must never be retried.
func (c *Client) Refresh(ctx context.Context, refreshToken string) (*TokenResponse, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("client_id", ClientID)
	form.Set("refresh_token", refreshToken)
	return c.postToken(ctx, form)
}

func (c *Client) postToken(ctx context.Context, form url.Values) (*TokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoints.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call token endpoint: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("read token response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		oe := &Error{Status: resp.StatusCode}
		// A non-JSON body leaves Code empty; Error() then reports the status code.
		_ = json.Unmarshal(body, oe)
		return nil, oe
	}
	var tr TokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return nil, fmt.Errorf("parse token response: %w", err)
	}
	if tr.IDToken == "" {
		return nil, fmt.Errorf("token response did not contain an id_token")
	}
	return &tr, nil
}

// expiryFromSeconds converts an expires_in value to an absolute timestamp.
// A non-positive value yields the zero time, meaning "unknown".
func expiryFromSeconds(now time.Time, seconds int) time.Time {
	if seconds <= 0 {
		return time.Time{}
	}
	return now.Add(time.Duration(seconds) * time.Second)
}
