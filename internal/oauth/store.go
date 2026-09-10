package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/baidubce/bce-cli/internal/sts"
)

// ErrNotLoggedIn is returned when no OAuth login state exists for a profile.
var ErrNotLoggedIn = errors.New("not logged in with OAuth; run `bce auth login`")

// TokenSet is the persisted OAuth login state of one profile.
//
// The file lives outside config.json so that credential material is never
// mixed into the user-editable configuration.
type TokenSet struct {
	IAMBase  string `json:"iam_base"`
	STSBase  string `json:"sts_base"`
	Scope    string `json:"scope,omitempty"`
	Username string `json:"username,omitempty"`

	AccessToken          string    `json:"access_token,omitempty"`
	AccessTokenExpiresAt time.Time `json:"access_token_expires_at"`

	RefreshToken          string    `json:"refresh_token,omitempty"`
	RefreshTokenExpiresAt time.Time `json:"refresh_token_expires_at"`

	IDToken          string    `json:"id_token,omitempty"`
	IDTokenExpiresAt time.Time `json:"id_token_expires_at"`

	STS *sts.Credentials `json:"sts,omitempty"`
}

// ApplyTokenResponse merges a token endpoint response into the token set,
// computing absolute expiry times. The refresh token is replaced only when the
// response carries one, since IAM omits it when offline_access was not granted.
func (t *TokenSet) ApplyTokenResponse(tr *TokenResponse, now time.Time) {
	t.AccessToken = tr.AccessToken
	t.AccessTokenExpiresAt = expiryFromSeconds(now, tr.ExpiresIn)
	t.IDToken = tr.IDToken
	if tr.Scope != "" {
		t.Scope = tr.Scope
	}
	if tr.RefreshToken != "" {
		t.RefreshToken = tr.RefreshToken
		t.RefreshTokenExpiresAt = expiryFromSeconds(now, tr.RefreshExpiresIn)
	}
	// Prefer the exp claim of the ID Token itself; fall back to expires_in.
	t.IDTokenExpiresAt = expiryFromSeconds(now, tr.ExpiresIn)
	if claims, err := DecodeIDToken(tr.IDToken); err == nil {
		if exp := claims.ExpiresAt(); !exp.IsZero() {
			t.IDTokenExpiresAt = exp
		}
		if claims.Username != "" {
			t.Username = claims.Username
		}
	}
	// Cached STS credentials belong to the previous ID Token.
	t.STS = nil
}

// tokenDir returns ~/.bce/oauth. It intentionally does not depend on the config
// package so that config can depend on this one.
func tokenDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".bce", "oauth"), nil
}

// TokenPath returns the token file path of a profile.
func TokenPath(profile string) (string, error) {
	dir, err := tokenDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, tokenFileName(profile)+".json"), nil
}

// tokenFileName maps a profile name to a safe single path segment.
func tokenFileName(profile string) string {
	out := make([]rune, 0, len(profile))
	for _, r := range profile {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			out = append(out, r)
		default:
			out = append(out, '_')
		}
	}
	if len(out) == 0 {
		return "default"
	}
	return string(out)
}

// LoadTokens reads the persisted login state of a profile.
// Returns ErrNotLoggedIn when no state exists.
func LoadTokens(profile string) (*TokenSet, error) {
	path, err := TokenPath(profile)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, ErrNotLoggedIn
	}
	if err != nil {
		return nil, fmt.Errorf("read OAuth tokens: %w", err)
	}
	var ts TokenSet
	if err := json.Unmarshal(data, &ts); err != nil {
		return nil, fmt.Errorf("parse OAuth tokens (%s): %w", path, err)
	}
	return &ts, nil
}

// SaveTokens writes the login state atomically with 0600 permissions.
func SaveTokens(profile string, ts *TokenSet) error {
	path, err := TokenPath(profile)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("create OAuth token dir: %w", err)
	}
	data, err := json.MarshalIndent(ts, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tokens-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp token file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once the rename succeeds
	// CreateTemp already uses 0600, but be explicit: this file holds secrets.
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod temp token file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp token file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync temp token file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp token file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("persist OAuth tokens: %w", err)
	}
	return nil
}

// DeleteTokens removes the login state of a profile. Missing state is not an error.
func DeleteTokens(profile string) error {
	path, err := TokenPath(profile)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove OAuth tokens: %w", err)
	}
	return nil
}

const (
	lockRetryInterval = 50 * time.Millisecond
	lockWaitTimeout   = 120 * time.Second
	// lockStaleAfter must stay comfortably above the worst-case time the lock is
	// held (discovery + refresh + STS exchange, each bounded by the HTTP client
	// timeout). Reclaiming a live lock too early would let two processes refresh
	// the same rotating refresh token and get the whole token family revoked.
	lockStaleAfter = 5 * time.Minute
)

// withLock serialises token mutations for one profile across concurrent bce
// processes. Refresh tokens are rotated by IAM, so two concurrent refreshes of
// the same token would revoke the whole token family.
//
// The wait honours ctx: blocking past the caller's deadline would only lead to
// HTTP calls that fail immediately after the lock is finally acquired.
func withLock(ctx context.Context, profile string, fn func() error) error {
	path, err := TokenPath(profile)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("create OAuth token dir: %w", err)
	}
	lockPath := path + ".lock"

	deadline := time.Now().Add(lockWaitTimeout)
	for {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err == nil {
			f.Close()
			break
		}
		if !os.IsExist(err) {
			return fmt.Errorf("acquire OAuth token lock: %w", err)
		}
		// Reclaim a lock left behind by a crashed process.
		if fi, statErr := os.Stat(lockPath); statErr == nil && time.Since(fi.ModTime()) > lockStaleAfter {
			_ = os.Remove(lockPath)
			continue
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for OAuth token lock %s; remove it if no other bce process is running", lockPath)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for OAuth token lock %s: %w", lockPath, ctx.Err())
		case <-time.After(lockRetryInterval):
		}
	}
	defer os.Remove(lockPath)

	return fn()
}
