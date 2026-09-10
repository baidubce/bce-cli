package oauth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestInsecureTLSParsesEnv(t *testing.T) {
	cases := map[string]bool{
		"":        false,
		"true":    true,
		"TRUE":    true,
		"1":       true,
		"t":       true,
		"false":   false,
		"0":       false,
		"yes":     false, // not a Go boolean literal
		"garbage": false,
	}
	for value, want := range cases {
		t.Setenv(insecureTLSEnv, value)
		if got := InsecureTLS(); got != want {
			t.Errorf("%s=%q: InsecureTLS() = %v, want %v", insecureTLSEnv, value, got, want)
		}
	}
}

func TestNewHTTPClientVerifiesByDefault(t *testing.T) {
	t.Setenv(insecureTLSEnv, "")

	client := newHTTPClient(5 * time.Second)
	if client.Timeout != 5*time.Second {
		t.Errorf("timeout = %v, want 5s", client.Timeout)
	}
	if client.Transport != nil {
		t.Errorf("Transport = %#v, want the default transport (nil)", client.Transport)
	}
}

func TestNewHTTPClientSkipsVerificationWhenEnabled(t *testing.T) {
	t.Setenv(insecureTLSEnv, "true")

	client := newHTTPClient(5 * time.Second)
	tr, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport = %#v, want *http.Transport", client.Transport)
	}
	if tr.TLSClientConfig == nil || !tr.TLSClientConfig.InsecureSkipVerify {
		t.Error("InsecureSkipVerify was not enabled")
	}
	// Cloned from the default transport, so proxy handling survives.
	if tr.Proxy == nil {
		t.Error("Proxy was dropped; the default transport was not cloned")
	}
}

func TestNewHTTPClientAgainstSelfSignedServer(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	t.Setenv(insecureTLSEnv, "")
	if _, err := newHTTPClient(5 * time.Second).Get(srv.URL); err == nil {
		t.Error("expected the self-signed certificate to be rejected by default")
	}

	t.Setenv(insecureTLSEnv, "true")
	resp, err := newHTTPClient(5 * time.Second).Get(srv.URL)
	if err != nil {
		t.Fatalf("request with verification disabled failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}
