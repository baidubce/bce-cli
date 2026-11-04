package oauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/baidubce/bce-cli/internal/sts"
)

// stsServer serves the STS sessionToken/idToken endpoint, recording how many
// times it was called and which bearer token it received.
func stsServer(t *testing.T, calls *int, lastIDToken *string, ttl time.Duration) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*calls++
		if got := r.Header.Get("Authorization"); len(got) > 7 {
			*lastIDToken = got[7:]
		}
		exp := time.Now().Add(ttl).UTC().Format(time.RFC3339)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"accessKeyId":"ak","secretAccessKey":"sk","sessionToken":"token",` +
			`"expiration":"` + exp + `","userId":"u"}`))
	}))
}

func newTestManager(t *testing.T, profile string) *Manager {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	_ = os.Setenv("HOME", home)
	m := NewManager(profile)
	m.HTTP = &http.Client{Timeout: 5 * time.Second}
	return m
}

func TestCredentialsReusesValidCachedSTS(t *testing.T) {
	m := newTestManager(t, "p")
	calls := 0
	last := ""
	srv := stsServer(t, &calls, &last, time.Hour)
	defer srv.Close()

	if err := SaveTokens("p", &TokenSet{
		IAMBase: "https://iam.invalid",
		STSBase: srv.URL,
		STS: &sts.Credentials{
			AccessKeyID: "cached", SecretAccessKey: "sk", SessionToken: "token",
			Expiration: time.Now().Add(time.Hour),
		},
	}); err != nil {
		t.Fatalf("SaveTokens: %v", err)
	}

	creds, err := m.Credentials(context.Background())
	if err != nil {
		t.Fatalf("Credentials: %v", err)
	}
	if creds.AccessKeyID != "cached" {
		t.Errorf("access key = %q, want the cached one", creds.AccessKeyID)
	}
	if calls != 0 {
		t.Errorf("STS was called %d times, want 0 for valid cached credentials", calls)
	}
}

func TestCredentialsReExchangesWhenSTSNearExpiry(t *testing.T) {
	m := newTestManager(t, "p")
	calls := 0
	last := ""
	srv := stsServer(t, &calls, &last, time.Hour)
	defer srv.Close()

	idToken := fakeIDToken(t, map[string]any{"exp": time.Now().Add(time.Hour).Unix()})
	if err := SaveTokens("p", &TokenSet{
		IAMBase:          "https://iam.invalid",
		STSBase:          srv.URL,
		IDToken:          idToken,
		IDTokenExpiresAt: time.Now().Add(time.Hour),
		STS: &sts.Credentials{
			AccessKeyID: "stale", SecretAccessKey: "sk", SessionToken: "token",
			// Inside the 5-minute refresh window.
			Expiration: time.Now().Add(time.Minute),
		},
	}); err != nil {
		t.Fatalf("SaveTokens: %v", err)
	}

	creds, err := m.Credentials(context.Background())
	if err != nil {
		t.Fatalf("Credentials: %v", err)
	}
	if creds.AccessKeyID != "ak" {
		t.Errorf("access key = %q, want freshly issued", creds.AccessKeyID)
	}
	if calls != 1 {
		t.Errorf("STS called %d times, want 1", calls)
	}
	if last != idToken {
		t.Error("STS was not called with the stored id_token")
	}

	// The new credentials must have been persisted.
	ts, err := LoadTokens("p")
	if err != nil {
		t.Fatalf("LoadTokens: %v", err)
	}
	if ts.STS == nil || ts.STS.AccessKeyID != "ak" {
		t.Errorf("refreshed credentials were not persisted: %+v", ts.STS)
	}
}

func TestCredentialsRefreshesExpiredIDToken(t *testing.T) {
	m := newTestManager(t, "p")
	calls := 0
	last := ""
	stsSrv := stsServer(t, &calls, &last, time.Hour)
	defer stsSrv.Close()

	newIDToken := fakeIDToken(t, map[string]any{"exp": time.Now().Add(time.Hour).Unix(), "username": "carol"})
	var refreshForm string
	iamSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		refreshForm = r.PostForm.Get("refresh_token")
		w.Header().Set("Content-Type", "application/json")
		// Rotation: a brand new refresh token replaces the old one.
		_, _ = w.Write([]byte(`{"access_token":"at","expires_in":3600,"refresh_token":"rt-2",` +
			`"refresh_expires_in":2592000,"id_token":"` + newIDToken + `"}`))
	}))
	defer iamSrv.Close()

	if err := SaveTokens("p", &TokenSet{
		IAMBase:               iamSrv.URL,
		STSBase:               stsSrv.URL,
		IDToken:               fakeIDToken(t, map[string]any{"exp": time.Now().Add(-time.Minute).Unix()}),
		IDTokenExpiresAt:      time.Now().Add(-time.Minute),
		RefreshToken:          "rt-1",
		RefreshTokenExpiresAt: time.Now().Add(24 * time.Hour),
	}); err != nil {
		t.Fatalf("SaveTokens: %v", err)
	}

	if _, err := m.Credentials(context.Background()); err != nil {
		t.Fatalf("Credentials: %v", err)
	}
	if refreshForm != "rt-1" {
		t.Errorf("refresh sent %q, want rt-1", refreshForm)
	}
	if last != newIDToken {
		t.Error("STS was not called with the refreshed id_token")
	}

	ts, err := LoadTokens("p")
	if err != nil {
		t.Fatalf("LoadTokens: %v", err)
	}
	if ts.RefreshToken != "rt-2" {
		t.Errorf("rotated refresh token = %q, want rt-2", ts.RefreshToken)
	}
	if ts.Username != "carol" {
		t.Errorf("username = %q, want carol", ts.Username)
	}
}

func TestCredentialsClearsStateOnInvalidGrant(t *testing.T) {
	m := newTestManager(t, "p")
	iamSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer iamSrv.Close()

	if err := SaveTokens("p", &TokenSet{
		IAMBase:               iamSrv.URL,
		STSBase:               "https://sts.invalid",
		IDTokenExpiresAt:      time.Now().Add(-time.Hour),
		RefreshToken:          "rt-1",
		RefreshTokenExpiresAt: time.Now().Add(24 * time.Hour),
	}); err != nil {
		t.Fatalf("SaveTokens: %v", err)
	}

	if _, err := m.Credentials(context.Background()); err == nil {
		t.Fatal("expected an error, got nil")
	}
	if _, err := LoadTokens("p"); err != ErrNotLoggedIn {
		t.Errorf("login state after invalid_grant = %v, want it cleared", err)
	}
}

func TestCredentialsWithoutLoginFails(t *testing.T) {
	m := newTestManager(t, "p")
	if _, err := m.Credentials(context.Background()); err != ErrNotLoggedIn {
		t.Errorf("Credentials without login = %v, want ErrNotLoggedIn", err)
	}
}

func TestStsUsable(t *testing.T) {
	now := time.Now()
	complete := func(c *sts.Credentials) *sts.Credentials {
		c.AccessKeyID, c.SecretAccessKey, c.SessionToken = "ak", "sk", "token"
		return c
	}

	cases := []struct {
		name  string
		creds *sts.Credentials
		want  bool
	}{
		{"nil", nil, false},
		{"incomplete", &sts.Credentials{Expiration: now.Add(time.Hour)}, false},
		{"no expiration", complete(&sts.Credentials{}), false},
		{"comfortably valid", complete(&sts.Credentials{Expiration: now.Add(time.Hour)}), true},
		{"inside refresh window", complete(&sts.Credentials{Expiration: now.Add(time.Minute)}), false},
		{"expired", complete(&sts.Credentials{Expiration: now.Add(-time.Minute)}), false},
		{
			// Whole lifetime shorter than the refresh window: reusable in its first half.
			name: "short lifetime, first half",
			creds: complete(&sts.Credentials{
				CreateTime: now.Add(-time.Minute),
				Expiration: now.Add(3 * time.Minute),
			}),
			want: true,
		},
		{
			name: "short lifetime, second half",
			creds: complete(&sts.Credentials{
				CreateTime: now.Add(-3 * time.Minute),
				Expiration: now.Add(time.Minute),
			}),
			want: false,
		},
	}
	for _, tc := range cases {
		if got := stsUsable(tc.creds); got != tc.want {
			t.Errorf("%s: stsUsable = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestCredentialsFailsWhenRefreshTokenExpired(t *testing.T) {
	m := newTestManager(t, "p")
	if err := SaveTokens("p", &TokenSet{
		IAMBase:               "https://iam.invalid",
		STSBase:               "https://sts.invalid",
		IDTokenExpiresAt:      time.Now().Add(-time.Hour),
		RefreshToken:          "rt",
		RefreshTokenExpiresAt: time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatalf("SaveTokens: %v", err)
	}
	if _, err := m.Credentials(context.Background()); err == nil {
		t.Fatal("expected an error for an expired refresh token")
	}
	if _, err := LoadTokens("p"); err != ErrNotLoggedIn {
		t.Errorf("login state = %v, want it cleared", err)
	}
}

// TestLoginEndToEnd drives the full flow against a fake IAM and STS, standing in
// for the browser by requesting the loopback callback directly.
func TestLoginEndToEnd(t *testing.T) {
	m := newTestManager(t, "p")
	stsCalls := 0
	lastIDToken := ""
	stsSrv := stsServer(t, &stsCalls, &lastIDToken, time.Hour)
	defer stsSrv.Close()

	idToken := fakeIDToken(t, map[string]any{
		"exp":            time.Now().Add(time.Hour).Unix(),
		"username":       "dave",
		"account_id":     "acc-1",
		"principal_type": "SUBUSER",
	})

	var gotVerifier, gotRedirect string
	iamSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/iam/cli/oauth2/token" {
			http.NotFound(w, r)
			return
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		gotVerifier = r.PostForm.Get("code_verifier")
		gotRedirect = r.PostForm.Get("redirect_uri")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"at","expires_in":3600,"refresh_token":"rt",` +
			`"refresh_expires_in":2592000,"scope":"openid bce_sts offline_access","id_token":"` + idToken + `"}`))
	}))
	defer iamSrv.Close()

	env, err := ResolveEndpoints(iamSrv.URL, stsSrv.URL)
	if err != nil {
		t.Fatalf("ResolveEndpoints: %v", err)
	}

	var redirectURI string
	ts, err := m.Login(context.Background(), LoginOptions{
		Env:     env,
		Timeout: 10 * time.Second,
		OnAuthorizationURL: func(raw string) {
			u, err := url.Parse(raw)
			if err != nil {
				t.Errorf("parse authorization URL: %v", err)
				return
			}
			q := u.Query()
			redirectURI = q.Get("redirect_uri")
			go func() {
				resp, err := http.Get(redirectURI + "?code=the-code&state=" + url.QueryEscape(q.Get("state")))
				if err == nil {
					resp.Body.Close()
				}
			}()
		},
	})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	if gotVerifier == "" {
		t.Error("token request did not carry a code_verifier")
	}
	if gotRedirect != redirectURI {
		t.Errorf("token redirect_uri = %q, want the authorization one %q", gotRedirect, redirectURI)
	}
	if !strings.HasPrefix(redirectURI, "http://127.0.0.1:") || !strings.HasSuffix(redirectURI, "/callback") {
		t.Errorf("redirect_uri = %q, want http://127.0.0.1:{port}/callback", redirectURI)
	}
	if lastIDToken != idToken {
		t.Error("STS was not called with the freshly issued id_token")
	}
	if ts.STS == nil || ts.STS.AccessKeyID != "ak" {
		t.Errorf("login did not return STS credentials: %+v", ts.STS)
	}

	stored, err := LoadTokens("p")
	if err != nil {
		t.Fatalf("LoadTokens: %v", err)
	}
	if stored.RefreshToken != "rt" || stored.Username != "dave" || stored.IAMBase != iamSrv.URL {
		t.Errorf("unexpected persisted state: %+v", stored)
	}

	// The credentials just obtained must be reused without another STS call.
	before := stsCalls
	if _, err := m.Credentials(context.Background()); err != nil {
		t.Fatalf("Credentials: %v", err)
	}
	if stsCalls != before {
		t.Errorf("STS called again (%d -> %d) despite valid cached credentials", before, stsCalls)
	}
}

// TestLoginEndpointsSurviveRefresh guards the contract behind the hidden
// --iam-base / --sts-base flags: the addresses given at login are persisted and
// reused by later refreshes, which happen in other invocations where no flag is
// available.
func TestLoginEndpointsSurviveRefresh(t *testing.T) {
	m := newTestManager(t, "p")
	stsCalls := 0
	lastIDToken := ""
	stsSrv := stsServer(t, &stsCalls, &lastIDToken, time.Hour)
	defer stsSrv.Close()

	refreshedIDToken := fakeIDToken(t, map[string]any{"exp": time.Now().Add(time.Hour).Unix()})
	tokenCalls := 0
	var lastGrant string
	iamSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/iam/cli/oauth2/token" {
			http.NotFound(w, r)
			return
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		tokenCalls++
		lastGrant = r.PostForm.Get("grant_type")
		// The first call is the code exchange and hands out an already-expired ID
		// Token, so the next Credentials() call must go through a refresh.
		idToken := fakeIDToken(t, map[string]any{"exp": time.Now().Add(-time.Minute).Unix()})
		if tokenCalls > 1 {
			idToken = refreshedIDToken
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"at","expires_in":3600,"refresh_token":"rt",` +
			`"refresh_expires_in":2592000,"id_token":"` + idToken + `"}`))
	}))
	defer iamSrv.Close()

	env, err := ResolveEndpoints(iamSrv.URL, stsSrv.URL)
	if err != nil {
		t.Fatalf("ResolveEndpoints: %v", err)
	}
	if _, err := m.Login(context.Background(), LoginOptions{
		Env:     env,
		Timeout: 10 * time.Second,
		OnAuthorizationURL: func(raw string) {
			u, perr := url.Parse(raw)
			if perr != nil {
				t.Errorf("parse authorization URL: %v", perr)
				return
			}
			q := u.Query()
			go func() {
				resp, gerr := http.Get(q.Get("redirect_uri") + "?code=c&state=" + url.QueryEscape(q.Get("state")))
				if gerr == nil {
					resp.Body.Close()
				}
			}()
		},
	}); err != nil {
		t.Fatalf("Login: %v", err)
	}

	stored, err := LoadTokens("p")
	if err != nil {
		t.Fatalf("LoadTokens: %v", err)
	}
	if stored.IAMBase != iamSrv.URL || stored.STSBase != stsSrv.URL {
		t.Fatalf("login endpoints were not persisted: %+v", stored)
	}

	// Expire the cached credentials so renewal has to run.
	stored.STS.Expiration = time.Now().Add(time.Second)
	if err := SaveTokens("p", stored); err != nil {
		t.Fatalf("SaveTokens: %v", err)
	}

	stsBefore := stsCalls
	if _, err := m.Credentials(context.Background()); err != nil {
		t.Fatalf("Credentials: %v", err)
	}
	if tokenCalls < 2 || lastGrant != "refresh_token" {
		t.Errorf("refresh did not reach the login-time IAM base (token calls=%d, last grant=%q)", tokenCalls, lastGrant)
	}
	if stsCalls <= stsBefore || lastIDToken != refreshedIDToken {
		t.Errorf("STS renewal did not reach the login-time STS base with the refreshed id_token")
	}
}

func TestLoginRejectsIDTokenWithForeignNonce(t *testing.T) {
	m := newTestManager(t, "p")
	stsCalls := 0
	lastIDToken := ""
	stsSrv := stsServer(t, &stsCalls, &lastIDToken, time.Hour)
	defer stsSrv.Close()

	// An ID Token minted for a different login request.
	idToken := fakeIDToken(t, map[string]any{
		"exp":   time.Now().Add(time.Hour).Unix(),
		"nonce": "some-other-nonce",
	})
	iamSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"at","expires_in":3600,"id_token":"` + idToken + `"}`))
	}))
	defer iamSrv.Close()

	env, err := ResolveEndpoints(iamSrv.URL, stsSrv.URL)
	if err != nil {
		t.Fatalf("ResolveEndpoints: %v", err)
	}

	_, err = m.Login(context.Background(), LoginOptions{
		Env:     env,
		Timeout: 10 * time.Second,
		OnAuthorizationURL: func(raw string) {
			u, perr := url.Parse(raw)
			if perr != nil {
				t.Errorf("parse authorization URL: %v", perr)
				return
			}
			q := u.Query()
			go func() {
				resp, gerr := http.Get(q.Get("redirect_uri") + "?code=c&state=" + url.QueryEscape(q.Get("state")))
				if gerr == nil {
					resp.Body.Close()
				}
			}()
		},
	})
	if err == nil {
		t.Fatal("expected login to fail on a nonce mismatch")
	}
	if stsCalls != 0 {
		t.Errorf("STS was called %d times despite the nonce mismatch", stsCalls)
	}
	if _, err := LoadTokens("p"); err != ErrNotLoggedIn {
		t.Error("a rejected login must not persist any state")
	}
}
