package sts

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSessionTokenURL(t *testing.T) {
	cases := map[string]string{
		"https://sts.bj.baidubce.com":  "https://sts.bj.baidubce.com/v1/sessionToken/idToken",
		"https://sts.bj.baidubce.com/": "https://sts.bj.baidubce.com/v1/sessionToken/idToken",
		// Bases that already carry the /v1 prefix must not get a second one.
		"http://sts.example.internal:8586/v1":  "http://sts.example.internal:8586/v1/sessionToken/idToken",
		"http://sts.example.internal:8586/v1/": "http://sts.example.internal:8586/v1/sessionToken/idToken",
	}
	for base, want := range cases {
		if got := sessionTokenURL(base); got != want {
			t.Errorf("sessionTokenURL(%q) = %q, want %q", base, got, want)
		}
	}
}

func TestExchangeIDToken(t *testing.T) {
	var gotAuth, gotDuration, gotPath, gotBody, gotContentType string
	var gotContentLength int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotDuration = r.URL.Query().Get("durationSeconds")
		gotPath = r.URL.Path
		gotContentType = r.Header.Get("Content-Type")
		gotContentLength = r.ContentLength
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"accessKeyId":"ak","secretAccessKey":"sk","sessionToken":"token",` +
			`"expiration":"2026-08-19T12:00:00Z","createTime":"2026-08-19T11:00:00Z","userId":"u"}`))
	}))
	defer srv.Close()

	creds, err := ExchangeIDToken(context.Background(), srv.Client(), srv.URL, "the-id-token", 1800)
	if err != nil {
		t.Fatalf("ExchangeIDToken: %v", err)
	}
	if gotAuth != "Bearer the-id-token" {
		t.Errorf("Authorization = %q, want Bearer the-id-token", gotAuth)
	}
	if gotDuration != "1800" {
		t.Errorf("durationSeconds = %q, want 1800", gotDuration)
	}
	if gotPath != "/v1/sessionToken/idToken" {
		t.Errorf("path = %q", gotPath)
	}
	// IAM rejects an empty "{}" object, so the request must carry no body at all.
	if gotBody != "" {
		t.Errorf("body = %q, want it empty", gotBody)
	}
	if gotContentLength != 0 {
		t.Errorf("Content-Length = %d, want 0", gotContentLength)
	}
	if gotContentType != "" {
		t.Errorf("Content-Type = %q, want it unset for a bodyless request", gotContentType)
	}
	want := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	if !creds.Expiration.Equal(want) {
		t.Errorf("expiration = %v, want %v", creds.Expiration, want)
	}
}

func TestExchangeIDTokenDefaultsDuration(t *testing.T) {
	var gotDuration string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotDuration = r.URL.Query().Get("durationSeconds")
		_, _ = w.Write([]byte(`{"accessKeyId":"ak","secretAccessKey":"sk","sessionToken":"t","expiration":"2026-08-19T12:00:00Z"}`))
	}))
	defer srv.Close()

	if _, err := ExchangeIDToken(context.Background(), srv.Client(), srv.URL, "id", 0); err != nil {
		t.Fatalf("ExchangeIDToken: %v", err)
	}
	if gotDuration != "3600" {
		t.Errorf("durationSeconds = %q, want the 3600 default", gotDuration)
	}
}

func TestExchangeIDTokenSurfacesServiceError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"code":"AccessDenied","message":"scope missing","requestId":"req-1"}`))
	}))
	defer srv.Close()

	_, err := ExchangeIDToken(context.Background(), srv.Client(), srv.URL, "id", 3600)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if want := "scope missing"; err.Error() == "" || !contains(err.Error(), want) {
		t.Errorf("error %q does not include %q", err, want)
	}
}

func TestExchangeIDTokenRequiresIDToken(t *testing.T) {
	if _, err := ExchangeIDToken(context.Background(), http.DefaultClient, "http://sts.invalid", "", 3600); err == nil {
		t.Fatal("expected an error for an empty id_token")
	}
}

func TestExchangeIDTokenRejectsIncompleteResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"accessKeyId":"ak"}`))
	}))
	defer srv.Close()

	if _, err := ExchangeIDToken(context.Background(), srv.Client(), srv.URL, "id", 3600); err == nil {
		t.Fatal("expected an error when credential fields are missing")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
