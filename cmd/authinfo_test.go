package cmd

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/baidubce/bce-cli/internal/i18n"
	"github.com/baidubce/bce-cli/internal/oauth"
	"github.com/spf13/cobra"
)

func TestHumanizeDuration(t *testing.T) {
	cases := map[time.Duration]string{
		47*time.Hour + 53*time.Minute: "47h 53m",
		time.Hour:                     "1h 0m",
		12 * time.Minute:              "12m",
		90 * time.Second:              "1m",
		30 * time.Second:              "<1m",
		0:                             "<1m",
	}
	for d, want := range cases {
		if got := humanizeDuration(d); got != want {
			t.Errorf("humanizeDuration(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestSessionRemaining(t *testing.T) {
	now := time.Now()

	withRefresh := &oauth.TokenSet{
		RefreshToken:          "rt",
		RefreshTokenExpiresAt: now.Add(48 * time.Hour),
		IDTokenExpiresAt:      now.Add(time.Hour),
	}
	if d := sessionRemaining(withRefresh); d < 47*time.Hour {
		t.Errorf("refresh token lifetime should win, got %v", d)
	}

	// No refresh token: fall back to the ID Token deadline.
	idOnly := &oauth.TokenSet{IDTokenExpiresAt: now.Add(30 * time.Minute)}
	if d := sessionRemaining(idOnly); d > time.Hour || d < 20*time.Minute {
		t.Errorf("id token lifetime not used, got %v", d)
	}

	// Refresh token already expired but the ID Token is still usable: report the
	// deadline that actually keeps the session alive, matching oauth.ActiveLogin.
	staleRefresh := &oauth.TokenSet{
		RefreshToken:          "rt",
		RefreshTokenExpiresAt: now.Add(-time.Hour),
		IDTokenExpiresAt:      now.Add(30 * time.Minute),
	}
	if d := sessionRemaining(staleRefresh); d <= 0 || d > time.Hour {
		t.Errorf("expired refresh token must not hide the ID Token lifetime, got %v", d)
	}

	// Everything expired.
	if d := sessionRemaining(&oauth.TokenSet{
		RefreshToken:          "rt",
		RefreshTokenExpiresAt: now.Add(-time.Hour),
		IDTokenExpiresAt:      now.Add(-time.Hour),
	}); d != 0 {
		t.Errorf("expired session should yield 0, got %v", d)
	}

	if d := sessionRemaining(&oauth.TokenSet{}); d != 0 {
		t.Errorf("unknown deadline should yield 0, got %v", d)
	}
}

func TestBold(t *testing.T) {
	if got := bold("x", false); got != "x" {
		t.Errorf("bold disabled = %q, want plain text", got)
	}
	if got := bold("x", true); got != "\033[1mx\033[0m" {
		t.Errorf("bold enabled = %q", got)
	}
	if got := bold("", true); got != "" {
		t.Errorf("empty string must stay empty, got %q", got)
	}
}

func TestRed(t *testing.T) {
	if got := red("✗", false); got != "✗" {
		t.Errorf("red disabled = %q, want plain text", got)
	}
	if got := red("✗", true); got != "\033[0;31m✗\033[0m" {
		t.Errorf("red enabled = %q", got)
	}
}

func TestPrintAlreadyAuthenticated(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	ts := &oauth.TokenSet{
		IAMBase:               oauth.DefaultIAMBase,
		STSBase:               oauth.DefaultSTSBase,
		RefreshToken:          "rt",
		RefreshTokenExpiresAt: time.Now().Add(47*time.Hour + 53*time.Minute),
		IDToken:               idTokenWithClaims(t, `{"account_id":"1234567890abcdef","username":"tester"}`),
	}

	out := runPrintAlreadyAuthenticated(t, ts)
	for _, want := range []string{
		"✗ Already authenticated as 1234567890abcdef",
		"Token expires in 47h 5",
		"To re-login, run: bce auth logout first",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Credential source") || strings.Contains(out, "oauth.json") {
		t.Errorf("credential source must not be printed:\n%s", out)
	}
	if strings.HasPrefix(out, "\n") || strings.HasSuffix(out, "\n\n") || strings.Contains(out, "  ") {
		t.Errorf("notice must not have leading/trailing blank lines or indentation: %q", out)
	}
	if strings.Contains(out, "\033[") {
		t.Errorf("output must not be styled when writing to a buffer:\n%s", out)
	}
}

func TestPrintAlreadyAuthenticatedShowsGivenLogoutCommand(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	ts := &oauth.TokenSet{IDTokenExpiresAt: time.Now().Add(time.Hour)}
	out := runPrintAlreadyAuthenticatedWith(t, ts, "bce auth logout")
	if !strings.Contains(out, "To re-login, run: bce auth logout first") {
		t.Errorf("logout command not rendered verbatim:\n%s", out)
	}
}

func TestPrintAlreadyAuthenticatedFallsBackToUsername(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	ts := &oauth.TokenSet{
		IDToken:          idTokenWithClaims(t, `{"username":"tester"}`),
		IDTokenExpiresAt: time.Now().Add(time.Hour),
	}
	if out := runPrintAlreadyAuthenticated(t, ts); !strings.Contains(out, "authenticated as tester") {
		t.Errorf("username fallback missing:\n%s", out)
	}

	// Nothing identifying at all: the line degrades instead of printing "as ".
	bare := &oauth.TokenSet{IDTokenExpiresAt: time.Now().Add(time.Hour)}
	out := runPrintAlreadyAuthenticated(t, bare)
	if !strings.Contains(out, "Already authenticated") {
		t.Errorf("missing headline:\n%s", out)
	}
	if strings.Contains(out, "authenticated as") {
		t.Errorf("must not print an empty identity:\n%s", out)
	}
}

// idTokenWithClaims builds an unsigned JWT-shaped token; the CLI only decodes
// the payload locally, signature verification happens in STS.
func idTokenWithClaims(t *testing.T, claimsJSON string) string {
	t.Helper()
	enc := func(s string) string {
		return base64.RawURLEncoding.EncodeToString([]byte(s))
	}
	return enc(`{"alg":"RS256","typ":"JWT"}`) + "." + enc(claimsJSON) + ".sig"
}

// runPrintAlreadyAuthenticated renders the notice into a buffer using a command
// tree that carries the global flags the function reads. The language is pinned
// to en-US so assertions do not depend on the host locale.
func runPrintAlreadyAuthenticated(t *testing.T, ts *oauth.TokenSet) string {
	t.Helper()
	return runPrintAlreadyAuthenticatedWith(t, ts, "bce auth logout")
}

func runPrintAlreadyAuthenticatedWith(t *testing.T, ts *oauth.TokenSet, logoutCmd string) string {
	t.Helper()
	prev := i18n.GetLanguage()
	if err := i18n.SetLanguage("en-US"); err != nil {
		t.Fatalf("SetLanguage: %v", err)
	}
	t.Cleanup(func() { _ = i18n.SetLanguage(prev) })

	root := &cobra.Command{Use: "bce"}
	root.PersistentFlags().Bool("no-color", false, "")
	child := &cobra.Command{Use: "login"}
	root.AddCommand(child)

	var buf bytes.Buffer
	child.SetOut(&buf)
	printAlreadyAuthenticated(child, ts, logoutCmd)
	return buf.String()
}
