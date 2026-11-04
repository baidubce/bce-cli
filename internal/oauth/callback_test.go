package oauth

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestCallbackServerAcceptsMatchingState(t *testing.T) {
	cs, err := NewCallbackServer("the-state")
	if err != nil {
		t.Fatalf("NewCallbackServer: %v", err)
	}
	defer cs.Close()

	go func() {
		resp, err := http.Get(cs.RedirectURI() + "?code=abc123&state=the-state")
		if err == nil {
			resp.Body.Close()
		}
	}()

	code, err := cs.Wait(context.Background(), 5*time.Second)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if code != "abc123" {
		t.Errorf("code = %q, want abc123", code)
	}
}

func TestCallbackServerRejectsStateMismatch(t *testing.T) {
	cs, err := NewCallbackServer("expected-state")
	if err != nil {
		t.Fatalf("NewCallbackServer: %v", err)
	}
	defer cs.Close()

	go func() {
		resp, err := http.Get(cs.RedirectURI() + "?code=abc123&state=attacker-state")
		if err == nil {
			resp.Body.Close()
		}
	}()

	if _, err := cs.Wait(context.Background(), 5*time.Second); err == nil {
		t.Fatal("expected an error for mismatched state, got nil")
	}
}

func TestCallbackServerRejectsMissingCode(t *testing.T) {
	cs, err := NewCallbackServer("s")
	if err != nil {
		t.Fatalf("NewCallbackServer: %v", err)
	}
	defer cs.Close()

	go func() {
		resp, err := http.Get(cs.RedirectURI() + "?state=s")
		if err == nil {
			resp.Body.Close()
		}
	}()

	if _, err := cs.Wait(context.Background(), 5*time.Second); err == nil {
		t.Fatal("expected an error when no code is present, got nil")
	}
}

func TestCallbackServerOnlyHonoursFirstCallback(t *testing.T) {
	cs, err := NewCallbackServer("s")
	if err != nil {
		t.Fatalf("NewCallbackServer: %v", err)
	}
	defer cs.Close()

	resp, err := http.Get(cs.RedirectURI() + "?code=first&state=s")
	if err != nil {
		t.Fatalf("first callback: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("first callback status = %d, want 200", resp.StatusCode)
	}

	resp2, err := http.Get(cs.RedirectURI() + "?code=second&state=s")
	if err != nil {
		t.Fatalf("second callback: %v", err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusForbidden {
		t.Errorf("replayed callback status = %d, want 403", resp2.StatusCode)
	}

	code, err := cs.Wait(context.Background(), time.Second)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if code != "first" {
		t.Errorf("code = %q, want first", code)
	}
}

func TestCallbackServerPropagatesAuthorizationError(t *testing.T) {
	cs, err := NewCallbackServer("s")
	if err != nil {
		t.Fatalf("NewCallbackServer: %v", err)
	}
	defer cs.Close()

	go func() {
		resp, err := http.Get(cs.RedirectURI() + "?error=access_denied&error_description=user+cancelled&state=s")
		if err == nil {
			resp.Body.Close()
		}
	}()

	_, err = cs.Wait(context.Background(), 5*time.Second)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if want := "access_denied"; !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not mention %q", err, want)
	}
}
