package oauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// fakeIDToken builds an unsigned JWT-shaped token carrying the given claims.
// The CLI never verifies the signature locally, so this is enough for tests.
func fakeIDToken(t *testing.T, claims map[string]any) string {
	t.Helper()
	enc := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		return base64.RawURLEncoding.EncodeToString(b)
	}
	header := enc(map[string]string{"alg": "RS256", "typ": "JWT", "kid": "test"})
	return header + "." + enc(claims) + ".c2ln"
}

func TestAuthorizationURLParameters(t *testing.T) {
	c := &Client{Endpoints: Endpoints{AuthorizationEndpoint: "https://iam.example.com/api/iam/cli/oauth2/authorize"}}
	raw := c.AuthorizationURL("http://127.0.0.1:54321/callback", "st", "no", "ch")

	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse URL: %v", err)
	}
	q := u.Query()
	for k, want := range map[string]string{
		"response_type":         "code",
		"client_id":             ClientID,
		"redirect_uri":          "http://127.0.0.1:54321/callback",
		"scope":                 Scope,
		"state":                 "st",
		"nonce":                 "no",
		"code_challenge":        "ch",
		"code_challenge_method": "S256",
	} {
		if got := q.Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	if strings.Contains(raw, "code_verifier") {
		t.Error("authorization URL must never carry the code_verifier")
	}
}

func TestExchangeCodeSendsPKCEAndParsesResponse(t *testing.T) {
	idToken := fakeIDToken(t, map[string]any{"exp": time.Now().Add(time.Hour).Unix(), "username": "alice"})

	var gotForm url.Values
	var gotAuthHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		gotForm = r.PostForm
		gotAuthHeader = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"at","token_type":"Bearer","expires_in":3600,` +
			`"refresh_token":"rt","refresh_expires_in":2592000,"scope":"openid bce_sts offline_access",` +
			`"id_token":"` + idToken + `"}`))
	}))
	defer srv.Close()

	c := &Client{HTTP: srv.Client(), Endpoints: Endpoints{TokenEndpoint: srv.URL}}
	tr, err := c.ExchangeCode(context.Background(), "the-code", "http://127.0.0.1:1/callback", "the-verifier")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}

	for k, want := range map[string]string{
		"grant_type":    "authorization_code",
		"client_id":     ClientID,
		"code":          "the-code",
		"redirect_uri":  "http://127.0.0.1:1/callback",
		"code_verifier": "the-verifier",
	} {
		if got := gotForm.Get(k); got != want {
			t.Errorf("form %s = %q, want %q", k, got, want)
		}
	}
	if gotForm.Has("client_secret") {
		t.Error("token request must not send a client_secret")
	}
	if gotAuthHeader != "" {
		t.Errorf("token request must not send an Authorization header, got %q", gotAuthHeader)
	}
	if tr.RefreshToken != "rt" || tr.IDToken != idToken {
		t.Errorf("unexpected token response: %+v", tr)
	}
}

func TestRefreshDetectsInvalidGrant(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"token revoked"}`))
	}))
	defer srv.Close()

	c := &Client{HTTP: srv.Client(), Endpoints: Endpoints{TokenEndpoint: srv.URL}}
	_, err := c.Refresh(context.Background(), "old-rt")
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if !IsInvalidGrant(err) {
		t.Errorf("IsInvalidGrant(%v) = false, want true", err)
	}
}

func TestPostTokenRejectsResponseWithoutIDToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"access_token":"at","expires_in":3600}`))
	}))
	defer srv.Close()

	c := &Client{HTTP: srv.Client(), Endpoints: Endpoints{TokenEndpoint: srv.URL}}
	if _, err := c.Refresh(context.Background(), "rt"); err == nil {
		t.Fatal("expected an error when id_token is missing, got nil")
	}
}

func TestCacheTTL(t *testing.T) {
	cases := map[string]time.Duration{
		"":                           0,
		"no-store":                   0,
		"public, max-age=300":        300 * time.Second,
		"max-age=0":                  0,
		"public, no-cache":           0,
		"private, max-age=not-a-num": 0,
	}
	for header, want := range cases {
		if got := cacheTTL(header); got != want {
			t.Errorf("cacheTTL(%q) = %v, want %v", header, got, want)
		}
	}
}

func TestResolveEndpoints(t *testing.T) {
	def, err := ResolveEndpoints("", "")
	if err != nil {
		t.Fatalf("ResolveEndpoints: %v", err)
	}
	if def.IAMBase != DefaultIAMBase || def.STSBase != DefaultSTSBase {
		t.Errorf("unexpected defaults: %+v", def)
	}
	if !strings.HasPrefix(def.STSBase, "https://") {
		t.Errorf("default STS base %q is not TLS-protected", def.STSBase)
	}

	// Endpoints must not be configurable through the environment.
	t.Setenv("BCE_IAM_BASE", "https://iam.env")
	t.Setenv("BCE_STS_BASE", "https://sts.env")
	withEnv, err := ResolveEndpoints("", "")
	if err != nil {
		t.Fatalf("ResolveEndpoints(env): %v", err)
	}
	if withEnv.IAMBase != DefaultIAMBase || withEnv.STSBase != DefaultSTSBase {
		t.Errorf("environment variables must be ignored, got %+v", withEnv)
	}

	overridden, err := ResolveEndpoints("https://iam.test/", "https://sts.test/")
	if err != nil {
		t.Fatalf("ResolveEndpoints(overrides): %v", err)
	}
	if overridden.IAMBase != "https://iam.test" || overridden.STSBase != "https://sts.test" {
		t.Errorf("explicit arguments not applied or trailing slash kept: %+v", overridden)
	}
}

func TestStaticEndpointsLayout(t *testing.T) {
	eps := staticEndpoints("https://console.bce.baidu.com/")
	if eps.TokenEndpoint != "https://console.bce.baidu.com/api/iam/cli/oauth2/token" {
		t.Errorf("unexpected token endpoint: %q", eps.TokenEndpoint)
	}
}
