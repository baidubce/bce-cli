package oauth

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func setupTokenHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return filepath.Join(home, ".bce", "oauth")
}

func TestActiveLoginOnlyLooksAtNamedProfile(t *testing.T) {
	setupTokenHome(t)

	// A leftover active login under a different profile name must not be
	// reported for "oauth" — ActiveLogin only looks at the one profile it was
	// asked about.
	if err := SaveTokens("oauth-prod", &TokenSet{
		IAMBase:               DefaultIAMBase,
		STSBase:               DefaultSTSBase,
		RefreshToken:          "rt",
		RefreshTokenExpiresAt: time.Now().Add(48 * time.Hour),
	}); err != nil {
		t.Fatalf("SaveTokens: %v", err)
	}

	if ts := ActiveLogin("oauth"); ts != nil {
		t.Errorf("expected no active login for %q, got %v", "oauth", ts)
	}

	// Once "oauth" itself has a usable login, ActiveLogin must report it.
	if err := SaveTokens("oauth", &TokenSet{
		IAMBase:               DefaultIAMBase,
		STSBase:               DefaultSTSBase,
		RefreshToken:          "rt",
		RefreshTokenExpiresAt: time.Now().Add(48 * time.Hour),
	}); err != nil {
		t.Fatalf("SaveTokens: %v", err)
	}
	if ts := ActiveLogin("oauth"); ts == nil {
		t.Fatal("expected an active login for \"oauth\"")
	}
}

func TestActiveLoginAcceptsIDTokenOnlySession(t *testing.T) {
	setupTokenHome(t)

	if err := SaveTokens("oauth", &TokenSet{
		IAMBase:          DefaultIAMBase,
		STSBase:          DefaultSTSBase,
		IDToken:          "header.payload.sig",
		IDTokenExpiresAt: time.Now().Add(30 * time.Minute),
	}); err != nil {
		t.Fatalf("SaveTokens: %v", err)
	}

	if ts := ActiveLogin("oauth"); ts == nil {
		t.Fatal("expected an active login backed only by a usable ID Token")
	}
}

func TestActiveLoginTreatsExpiredAsAbsent(t *testing.T) {
	setupTokenHome(t)

	// A login whose refresh token has expired and whose ID Token is stale
	// must not count as active.
	if err := SaveTokens("oauth", &TokenSet{
		IAMBase:               DefaultIAMBase,
		STSBase:               DefaultSTSBase,
		RefreshToken:          "rt",
		RefreshTokenExpiresAt: time.Now().Add(-time.Hour),
		IDTokenExpiresAt:      time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatalf("SaveTokens: %v", err)
	}

	if ts := ActiveLogin("oauth"); ts != nil {
		t.Error("expired login must not be reported as active")
	}
}

func TestActiveLoginIgnoresCorruptState(t *testing.T) {
	dir := setupTokenHome(t)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "oauth.json"), []byte("not json"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if ts := ActiveLogin("oauth"); ts != nil {
		t.Errorf("corrupt state must be ignored, got %v", ts)
	}
}

func TestActiveLoginMissingProfile(t *testing.T) {
	setupTokenHome(t)

	if ts := ActiveLogin("oauth"); ts != nil {
		t.Error("expected nil for a profile with no token file")
	}
}
