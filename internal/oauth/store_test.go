package oauth

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/baidubce/bce-cli/internal/sts"
)

func TestSaveLoadDeleteTokens(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))

	if _, err := LoadTokens("default"); err != ErrNotLoggedIn {
		t.Fatalf("LoadTokens on empty state = %v, want ErrNotLoggedIn", err)
	}

	want := &TokenSet{
		IAMBase:      "https://iam.test",
		STSBase:      "https://sts.test",
		RefreshToken: "rt",
		IDToken:      "id",
		STS: &sts.Credentials{
			AccessKeyID:     "ak",
			SecretAccessKey: "sk",
			SessionToken:    "token",
			Expiration:      time.Now().Add(time.Hour).UTC().Truncate(time.Second),
		},
	}
	if err := SaveTokens("default", want); err != nil {
		t.Fatalf("SaveTokens: %v", err)
	}

	path, err := TokenPath("default")
	if err != nil {
		t.Fatalf("TokenPath: %v", err)
	}
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat token file: %v", err)
		}
		if perm := fi.Mode().Perm(); perm != 0600 {
			t.Errorf("token file mode = %o, want 600", perm)
		}
		di, err := os.Stat(filepath.Dir(path))
		if err != nil {
			t.Fatalf("stat token dir: %v", err)
		}
		if perm := di.Mode().Perm(); perm != 0700 {
			t.Errorf("token dir mode = %o, want 700", perm)
		}
	}

	got, err := LoadTokens("default")
	if err != nil {
		t.Fatalf("LoadTokens: %v", err)
	}
	if got.RefreshToken != "rt" || got.STS == nil || got.STS.AccessKeyID != "ak" {
		t.Errorf("round-trip mismatch: %+v", got)
	}
	if !got.STS.Expiration.Equal(want.STS.Expiration) {
		t.Errorf("expiration = %v, want %v", got.STS.Expiration, want.STS.Expiration)
	}

	// No temp files may survive an atomic write.
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatalf("read token dir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != filepath.Base(path) {
			t.Errorf("unexpected leftover file in token dir: %s", e.Name())
		}
	}

	if err := DeleteTokens("default"); err != nil {
		t.Fatalf("DeleteTokens: %v", err)
	}
	if err := DeleteTokens("default"); err != nil {
		t.Fatalf("DeleteTokens on missing state should be a no-op, got %v", err)
	}
	if _, err := LoadTokens("default"); err != ErrNotLoggedIn {
		t.Fatalf("LoadTokens after delete = %v, want ErrNotLoggedIn", err)
	}
}

func TestTokenFileNameIsASinglePathSegment(t *testing.T) {
	cases := map[string]string{
		"default":    "default",
		"prod-1":     "prod-1",
		"../../etc":  "______etc",
		"a/b":        "a_b",
		"":           "default",
		"..":         "__",
		"with space": "with_space",
	}
	for in, want := range cases {
		if got := tokenFileName(in); got != want {
			t.Errorf("tokenFileName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWithLockIsExclusive(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))

	path, err := TokenPath("p")
	if err != nil {
		t.Fatalf("TokenPath: %v", err)
	}

	err = withLock(context.Background(), "p", func() error {
		if _, statErr := os.Stat(path + ".lock"); statErr != nil {
			t.Errorf("lock file missing while the lock is held: %v", statErr)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("withLock: %v", err)
	}
	if _, statErr := os.Stat(path + ".lock"); !os.IsNotExist(statErr) {
		t.Error("lock file was not released")
	}
}

func TestWithLockHonoursContextCancellation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))

	path, err := TokenPath("p")
	if err != nil {
		t.Fatalf("TokenPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Simulate another live process holding the lock.
	f, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatalf("create lock: %v", err)
	}
	f.Close()
	defer os.Remove(path + ".lock")

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	called := false
	err = withLock(ctx, "p", func() error {
		called = true
		return nil
	})
	if err == nil {
		t.Fatal("expected withLock to fail once the context is done")
	}
	if called {
		t.Error("the guarded function must not run without the lock")
	}
}

func TestApplyTokenResponseKeepsOldRefreshTokenWhenAbsent(t *testing.T) {
	now := time.Now()
	ts := &TokenSet{RefreshToken: "old", RefreshTokenExpiresAt: now.Add(48 * time.Hour)}
	idToken := fakeIDToken(t, map[string]any{
		"exp":      now.Add(30 * time.Minute).Unix(),
		"username": "bob",
	})

	ts.ApplyTokenResponse(&TokenResponse{IDToken: idToken, ExpiresIn: 3600}, now)
	if ts.RefreshToken != "old" {
		t.Errorf("refresh token = %q, want it preserved", ts.RefreshToken)
	}
	if ts.Username != "bob" {
		t.Errorf("username = %q, want bob", ts.Username)
	}
	// The exp claim wins over expires_in.
	if want := now.Add(30 * time.Minute).Unix(); ts.IDTokenExpiresAt.Unix() != want {
		t.Errorf("id token expiry = %v, want unix %d", ts.IDTokenExpiresAt, want)
	}

	ts.ApplyTokenResponse(&TokenResponse{IDToken: idToken, ExpiresIn: 3600, RefreshToken: "new", RefreshExpiresIn: 60}, now)
	if ts.RefreshToken != "new" {
		t.Errorf("refresh token = %q, want new", ts.RefreshToken)
	}
}
