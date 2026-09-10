package cmd

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/baidubce/bce-cli/internal/config"
	"github.com/baidubce/bce-cli/internal/oauth"
	"github.com/baidubce/bce-cli/internal/sts"
	"github.com/spf13/cobra"
)

// fakeIDToken builds an unsigned JWT-shaped token; the CLI only decodes the
// payload locally, signature verification happens in STS.
func fakeIDToken(t *testing.T, claimsJSON string) string {
	t.Helper()
	enc := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	return enc(`{"alg":"RS256","typ":"JWT"}`) + "." + enc(claimsJSON) + ".sig"
}

// jsonHome points HOME at a fresh temp directory so config.json and the OAuth
// token store start empty for each test.
func jsonHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

// seedActiveLogin writes a usable login for the given profile directly to
// disk, bypassing the browser flow.
func seedActiveLogin(t *testing.T, profile string) *oauth.TokenSet {
	t.Helper()
	ts := &oauth.TokenSet{
		IAMBase:               oauth.DefaultIAMBase,
		STSBase:               oauth.DefaultSTSBase,
		RefreshToken:          "rt",
		RefreshTokenExpiresAt: time.Now().Add(48 * time.Hour),
		IDToken:               fakeIDToken(t, `{"account_id":"acct-1","username":"tester","principal_type":"ACCOUNT"}`),
		IDTokenExpiresAt:      time.Now().Add(time.Hour),
		Scope:                 "openid bce_sts offline_access",
		STS: &sts.Credentials{
			AccessKeyID: "ak", SecretAccessKey: "sk", SessionToken: "token",
			Expiration: time.Now().Add(time.Hour),
		},
	}
	if err := oauth.SaveTokens(profile, ts); err != nil {
		t.Fatalf("SaveTokens: %v", err)
	}
	return ts
}

// syncBuffer is a goroutine-safe bytes.Buffer, needed here because the CLI
// command runs on a background goroutine while the test polls its stderr.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func newSyncBuffer() *syncBuffer { return &syncBuffer{} }

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// runAuthJSON executes an auth subcommand's RunE with --format json set and
// captures stdout, without going through cobra's full argument parser (the
// commands read flags via cmd.Flags() directly).
func runAuthJSON(t *testing.T, c *cobra.Command, extraArgs ...string) map[string]any {
	t.Helper()
	root := &cobra.Command{Use: "bce"}
	root.PersistentFlags().String("profile", "", "")
	root.PersistentFlags().String("region", "", "")
	root.PersistentFlags().Bool("no-color", false, "")
	root.AddCommand(c)

	buf := captureOut(c)
	args := append([]string{c.Name(), "--format", "json"}, extraArgs...)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		t.Fatalf("%s --format json: %v", c.Name(), err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, buf.String())
	}
	return parsed
}

func freshAuthCommands() (*cobra.Command, *cobra.Command, *cobra.Command) {
	login := &cobra.Command{Use: "login", RunE: authLoginCmd.RunE}
	login.Flags().Bool("no-browser", false, "")
	login.Flags().String("iam-base", "", "")
	login.Flags().String("sts-base", "", "")
	login.Flags().Int("callback-timeout", 1, "")
	login.Flags().String("format", "text", "")

	status := &cobra.Command{Use: "status", RunE: authStatusCmd.RunE}
	status.Flags().String("format", "text", "")

	logout := &cobra.Command{Use: "logout", RunE: authLogoutCmd.RunE}
	logout.Flags().String("format", "text", "")

	return login, status, logout
}

func freshRefreshCommand() *cobra.Command {
	refresh := &cobra.Command{Use: "refresh", RunE: authRefreshCmd.RunE}
	refresh.Flags().String("format", "text", "")
	return refresh
}

func TestAuthStatusJSONNotLoggedIn(t *testing.T) {
	jsonHome(t)
	_, status, _ := freshAuthCommands()

	out := runAuthJSON(t, status)
	if out["authenticated"] != false {
		t.Errorf("authenticated = %v, want false", out["authenticated"])
	}
	if out["message"] != "Not logged in" {
		t.Errorf("message = %v", out["message"])
	}
	if _, ok := out["success"]; ok {
		t.Error("status output must not include a success field")
	}
}

func TestAuthStatusJSONLoggedIn(t *testing.T) {
	jsonHome(t)
	seedActiveLogin(t, defaultOAuthProfile)
	_, status, _ := freshAuthCommands()

	out := runAuthJSON(t, status)
	if out["authenticated"] != true {
		t.Errorf("authenticated = %v, want true", out["authenticated"])
	}
	if out["profile"] != defaultOAuthProfile {
		t.Errorf("profile = %v, want %q", out["profile"], defaultOAuthProfile)
	}
	if out["account_id"] != "acct-1" {
		t.Errorf("account_id = %v", out["account_id"])
	}
	if out["iam_base"] != oauth.DefaultIAMBase || out["sts_base"] != oauth.DefaultSTSBase {
		t.Errorf("endpoints = %v / %v", out["iam_base"], out["sts_base"])
	}
	if out["credentials_expire"] == nil || out["credentials_expire"] == "" {
		t.Error("credentials_expire missing")
	}
	// config.Load() defaults Current to "default" when config.json does not
	// exist yet, so this exercises the "not current" notice.
	if out["current_profile"] != "default" {
		t.Errorf("current_profile = %v, want %q", out["current_profile"], "default")
	}
}

func TestAuthStatusJSONLoggedInCurrentMatchesOmitsNotice(t *testing.T) {
	jsonHome(t)
	seedActiveLogin(t, defaultOAuthProfile)
	if err := config.Save(&config.Configuration{Current: defaultOAuthProfile}); err != nil {
		t.Fatalf("config.Save: %v", err)
	}
	_, status, _ := freshAuthCommands()

	out := runAuthJSON(t, status)
	if _, ok := out["current_profile"]; ok {
		t.Errorf("current_profile must be omitted when it matches the oauth profile: %v", out)
	}
}

func TestAuthLogoutJSONNotLoggedIn(t *testing.T) {
	jsonHome(t)
	_, _, logout := freshAuthCommands()

	out := runAuthJSON(t, logout)
	if out["success"] != true {
		t.Errorf("success = %v, want true", out["success"])
	}
	if out["authenticated"] != false {
		t.Errorf("authenticated = %v, want false", out["authenticated"])
	}
	if out["message"] != "Not logged in" {
		t.Errorf("message = %v", out["message"])
	}
}

func TestAuthLogoutJSONLoggedIn(t *testing.T) {
	jsonHome(t)
	seedActiveLogin(t, defaultOAuthProfile)
	_, _, logout := freshAuthCommands()

	out := runAuthJSON(t, logout)
	if out["success"] != true {
		t.Errorf("success = %v, want true", out["success"])
	}
	if out["message"] != "Logged out" {
		t.Errorf("message = %v", out["message"])
	}
	if out["authenticated"] != false {
		t.Errorf("authenticated = %v, want false", out["authenticated"])
	}

	if _, err := oauth.NewManager(defaultOAuthProfile).Status(); err != oauth.ErrNotLoggedIn {
		t.Errorf("token file was not removed: %v", err)
	}
}

func TestAuthLoginJSONAlreadyAuthenticated(t *testing.T) {
	jsonHome(t)
	seedActiveLogin(t, defaultOAuthProfile)
	// The "already authenticated" refusal only applies while oauth is also
	// the current profile; Load() defaults Current to "default" otherwise.
	if err := config.Save(&config.Configuration{Current: defaultOAuthProfile}); err != nil {
		t.Fatalf("config.Save: %v", err)
	}
	login, _, _ := freshAuthCommands()

	out := runAuthJSON(t, login)
	if out["success"] != false {
		t.Errorf("success = %v, want false", out["success"])
	}
	if out["authenticated"] != true {
		t.Errorf("authenticated = %v, want true", out["authenticated"])
	}
	msg, _ := out["message"].(string)
	if !strings.Contains(msg, "Already logged in as acct-1") || !strings.Contains(msg, "bce auth logout") {
		t.Errorf("message = %q", msg)
	}
	if out["token_expires"] == nil || out["token_expires"] == "" {
		t.Error("token_expires missing")
	}
	if out["token_remaining"] == nil || out["token_remaining"] == "" {
		t.Error("token_remaining missing")
	}
}

// TestAuthLoginJSONActiveButNotCurrentStartsFreshLogin covers the case where
// an active oauth login exists on disk but the user has since switched
// current away from it (e.g. back to an AK profile). login must not print
// the "Already authenticated" refusal in that case — it should proceed with
// a normal browser flow, since the whole point of running `bce auth login`
// here is to make oauth the current profile again.
func TestAuthLoginJSONActiveButNotCurrentStartsFreshLogin(t *testing.T) {
	home := jsonHome(t)
	seedActiveLogin(t, defaultOAuthProfile)

	// Point current at an unrelated AK profile.
	cfg := &config.Configuration{
		Current: "default",
		Profiles: []config.Profile{
			{Name: "default", Mode: config.ModeAK, AccessKeyId: "ak", SecretAccessKey: "sk"},
		},
	}
	if err := config.Save(cfg); err != nil {
		t.Fatalf("config.Save: %v", err)
	}

	idToken := fakeIDToken(t, `{"exp":`+strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10)+
		`,"account_id":"acct-3","username":"tester3","principal_type":"ACCOUNT"}`)
	iamSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/iam/cli/oauth2/token" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"at","expires_in":3600,"refresh_token":"rt2",` +
			`"refresh_expires_in":2592000,"scope":"openid bce_sts offline_access","id_token":"` + idToken + `"}`))
	}))
	defer iamSrv.Close()
	stsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"accessKeyId":"ak2","secretAccessKey":"sk2","sessionToken":"token2",` +
			`"expiration":"` + time.Now().Add(time.Hour).UTC().Format(time.RFC3339) + `"}`))
	}))
	defer stsSrv.Close()

	login, _, _ := freshAuthCommands()
	root := &cobra.Command{Use: "bce"}
	root.PersistentFlags().String("profile", "", "")
	root.PersistentFlags().String("region", "", "")
	root.PersistentFlags().Bool("no-color", false, "")
	root.AddCommand(login)
	stdout := captureOut(login)
	stderr := newSyncBuffer()
	login.SetErr(stderr)
	root.SetArgs([]string{"login", "--format", "json", "--no-browser",
		"--iam-base", iamSrv.URL, "--sts-base", stsSrv.URL, "--callback-timeout", "5"})

	done := make(chan error, 1)
	go func() { done <- root.Execute() }()

	var redirectURI, state string
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if url := extractLine(stderr.String(), "response_type=code"); url != "" {
			redirectURI = extractQueryParam(url, "redirect_uri")
			state = extractQueryParam(url, "state")
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if redirectURI == "" {
		t.Fatal("expected a fresh browser login, but no authorization URL appeared on stderr")
	}
	resp, err := http.Get(redirectURI + "?code=c&state=" + state)
	if err != nil {
		t.Fatalf("drive callback: %v", err)
	}
	resp.Body.Close()

	if err := <-done; err != nil {
		t.Fatalf("login --format json: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, stdout.String())
	}
	if parsed["success"] != true || parsed["authenticated"] != true {
		t.Fatalf("unexpected result for a successful login: %+v", parsed)
	}
	if parsed["account_id"] != "acct-3" {
		t.Errorf("account_id = %v, want acct-3 (the freshly logged-in account)", parsed["account_id"])
	}

	// current must now point back at the oauth profile.
	data, err := os.ReadFile(filepath.Join(home, ".bce", "config.json"))
	if err != nil {
		t.Fatalf("read config.json: %v", err)
	}
	var savedCfg config.Configuration
	if err := json.Unmarshal(data, &savedCfg); err != nil {
		t.Fatalf("parse config.json: %v", err)
	}
	if savedCfg.Current != defaultOAuthProfile {
		t.Errorf("current = %q, want %q", savedCfg.Current, defaultOAuthProfile)
	}
}

func TestAuthLoginJSONSuccess(t *testing.T) {
	jsonHome(t)

	idToken := fakeIDToken(t, `{"exp":`+strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10)+
		`,"account_id":"acct-2","username":"tester2","principal_type":"ACCOUNT"}`)
	iamSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/iam/cli/oauth2/token" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"at","expires_in":3600,"refresh_token":"rt",` +
			`"refresh_expires_in":2592000,"scope":"openid bce_sts offline_access","id_token":"` + idToken + `"}`))
	}))
	defer iamSrv.Close()
	stsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"accessKeyId":"ak","secretAccessKey":"sk","sessionToken":"token",` +
			`"expiration":"` + time.Now().Add(time.Hour).UTC().Format(time.RFC3339) + `"}`))
	}))
	defer stsSrv.Close()

	login, _, _ := freshAuthCommands()
	root := &cobra.Command{Use: "bce"}
	root.PersistentFlags().String("profile", "", "")
	root.PersistentFlags().String("region", "", "")
	root.PersistentFlags().Bool("no-color", false, "")
	root.AddCommand(login)
	stdout := captureOut(login)
	stderr := newSyncBuffer()
	login.SetErr(stderr)
	root.SetArgs([]string{"login", "--format", "json", "--no-browser",
		"--iam-base", iamSrv.URL, "--sts-base", stsSrv.URL, "--callback-timeout", "5"})

	// --format json suppresses the authorization URL on stdout (it must stay
	// pure JSON), but the notice still goes to stderr; poll it there and drive
	// the loopback callback ourselves, standing in for the browser.
	done := make(chan error, 1)
	go func() { done <- root.Execute() }()

	var redirectURI, state string
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if url := extractLine(stderr.String(), "response_type=code"); url != "" {
			redirectURI = extractQueryParam(url, "redirect_uri")
			state = extractQueryParam(url, "state")
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if redirectURI == "" {
		t.Fatal("authorization URL never appeared on stderr")
	}
	resp, err := http.Get(redirectURI + "?code=c&state=" + state)
	if err != nil {
		t.Fatalf("drive callback: %v", err)
	}
	resp.Body.Close()

	if err := <-done; err != nil {
		t.Fatalf("login --format json: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, stdout.String())
	}
	if parsed["success"] != true || parsed["authenticated"] != true {
		t.Fatalf("unexpected result for a successful login: %+v", parsed)
	}
	if parsed["account_id"] != "acct-2" {
		t.Errorf("account_id = %v", parsed["account_id"])
	}
	if parsed["profile"] != defaultOAuthProfile {
		t.Errorf("profile = %v, want %q", parsed["profile"], defaultOAuthProfile)
	}
}

// extractLine returns the first line of s containing sub, or "".
func extractLine(s, sub string) string {
	for _, line := range strings.Split(s, "\n") {
		if strings.Contains(line, sub) {
			return strings.TrimSpace(line)
		}
	}
	return ""
}

// extractQueryParam pulls a single query parameter out of a raw URL string
// without importing net/url just for this test helper.
func extractQueryParam(rawURL, key string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Query().Get(key)
}

func TestAuthLoginJSONTimeout(t *testing.T) {
	home := jsonHome(t)

	iamSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r) // never reached: the callback times out first
	}))
	defer iamSrv.Close()
	stsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer stsSrv.Close()

	login, _, _ := freshAuthCommands()
	root := &cobra.Command{Use: "bce"}
	root.PersistentFlags().String("profile", "", "")
	root.PersistentFlags().String("region", "", "")
	root.PersistentFlags().Bool("no-color", false, "")
	root.AddCommand(login)
	buf := captureOut(login)
	root.SetArgs([]string{"login", "--format", "json", "--no-browser",
		"--iam-base", iamSrv.URL, "--sts-base", stsSrv.URL, "--callback-timeout", "1"})

	// No callback is ever driven, so Manager.Login times out waiting for one;
	// this exercises the JSON error contract for that path.
	if err := root.Execute(); err != nil {
		t.Fatalf("login --format json must not return an error even on failure: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, buf.String())
	}
	if parsed["success"] != false || parsed["authenticated"] != false {
		t.Errorf("unexpected result for a timed-out login: %+v", parsed)
	}
	if msg, _ := parsed["message"].(string); msg != "Authorization timeout." {
		t.Errorf("message = %q, want %q", msg, "Authorization timeout.")
	}

	// No profile should have been persisted after a failed login.
	data, err := os.ReadFile(filepath.Join(home, ".bce", "config.json"))
	if err == nil && strings.Contains(string(data), defaultOAuthProfile) {
		t.Errorf("a failed login must not write config.json: %s", data)
	}
}

func TestAuthLoginJSONInvalidFormat(t *testing.T) {
	jsonHome(t)
	login, _, _ := freshAuthCommands()
	root := &cobra.Command{Use: "bce"}
	root.PersistentFlags().String("profile", "", "")
	root.PersistentFlags().String("region", "", "")
	root.PersistentFlags().Bool("no-color", false, "")
	root.AddCommand(login)
	root.SetArgs([]string{"login", "--format", "yaml"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected an error for an invalid --format value")
	}
}

func TestAuthRefreshJSONNotLoggedIn(t *testing.T) {
	jsonHome(t)
	refresh := freshRefreshCommand()

	out := runAuthJSON(t, refresh)
	if out["success"] != false {
		t.Errorf("success = %v, want false", out["success"])
	}
	if out["authenticated"] != false {
		t.Errorf("authenticated = %v, want false", out["authenticated"])
	}
	if out["message"] != "Not logged in" {
		t.Errorf("message = %v", out["message"])
	}
}

func TestAuthRefreshJSONSuccess(t *testing.T) {
	jsonHome(t)

	// STS server hands out a fresh, far-future expiry so the renewed
	// credentials differ from the stale seeded ones.
	newExpiry := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	stsCalls := 0
	stsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stsCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"accessKeyId":"ak2","secretAccessKey":"sk2","sessionToken":"token2",` +
			`"expiration":"` + newExpiry.Format(time.RFC3339) + `"}`))
	}))
	defer stsSrv.Close()

	// A login whose ID Token is still usable but whose STS credentials have
	// expired: refresh must exchange the ID Token for new STS credentials.
	ts := &oauth.TokenSet{
		IAMBase:               oauth.DefaultIAMBase,
		STSBase:               stsSrv.URL,
		RefreshToken:          "rt",
		RefreshTokenExpiresAt: time.Now().Add(48 * time.Hour),
		IDToken:               fakeIDToken(t, `{"account_id":"acct-1","username":"tester","principal_type":"ACCOUNT"}`),
		IDTokenExpiresAt:      time.Now().Add(time.Hour),
		Scope:                 "openid bce_sts offline_access",
		STS: &sts.Credentials{
			AccessKeyID: "old", SecretAccessKey: "old", SessionToken: "old",
			Expiration: time.Now().Add(-time.Hour),
		},
	}
	if err := oauth.SaveTokens(defaultOAuthProfile, ts); err != nil {
		t.Fatalf("SaveTokens: %v", err)
	}

	refresh := freshRefreshCommand()
	out := runAuthJSON(t, refresh)
	if out["success"] != true {
		t.Errorf("success = %v, want true", out["success"])
	}
	if out["authenticated"] != true {
		t.Errorf("authenticated = %v, want true", out["authenticated"])
	}
	gotExpiry, _ := out["credentials_expire"].(string)
	parsed, err := time.Parse(time.RFC3339, gotExpiry)
	if err != nil {
		t.Fatalf("credentials_expire %q is not RFC3339: %v", gotExpiry, err)
	}
	if !parsed.Equal(newExpiry) {
		t.Errorf("credentials_expire = %v, want %v (renewed)", parsed.UTC(), newExpiry)
	}
	if stsCalls == 0 {
		t.Error("refresh must call the STS endpoint")
	}

	// The renewed credentials must be persisted.
	reloaded, err := oauth.NewManager(defaultOAuthProfile).Status()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.STS == nil || reloaded.STS.AccessKeyID != "ak2" {
		t.Errorf("token file was not updated with renewed credentials: %+v", reloaded.STS)
	}
}

// TestAuthRefreshJSONReusesValidCredentials pins the behaviour that
// distinguishes refresh from a forced renewal: when the cached STS credentials
// are still comfortably valid, refresh reports success without making any STS
// network call.
func TestAuthRefreshJSONReusesValidCredentials(t *testing.T) {
	jsonHome(t)

	stsCalls := 0
	stsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stsCalls++
		http.Error(w, "should not be called", http.StatusInternalServerError)
	}))
	defer stsSrv.Close()

	ts := &oauth.TokenSet{
		IAMBase:               oauth.DefaultIAMBase,
		STSBase:               stsSrv.URL,
		RefreshToken:          "rt",
		RefreshTokenExpiresAt: time.Now().Add(48 * time.Hour),
		IDToken:               fakeIDToken(t, `{"account_id":"acct-1","username":"tester","principal_type":"ACCOUNT"}`),
		IDTokenExpiresAt:      time.Now().Add(time.Hour),
		Scope:                 "openid bce_sts offline_access",
		STS: &sts.Credentials{
			AccessKeyID: "valid", SecretAccessKey: "sk", SessionToken: "token",
			Expiration: time.Now().Add(time.Hour),
		},
	}
	if err := oauth.SaveTokens(defaultOAuthProfile, ts); err != nil {
		t.Fatalf("SaveTokens: %v", err)
	}

	refresh := freshRefreshCommand()
	out := runAuthJSON(t, refresh)
	if out["success"] != true || out["authenticated"] != true {
		t.Errorf("unexpected result: %+v", out)
	}
	if stsCalls != 0 {
		t.Errorf("refresh must reuse still-valid credentials without an STS call, got %d calls", stsCalls)
	}
}

// Ensure config package is referenced so goimports/staticcheck don't flag an
// unused import if test helpers above change.
var _ = config.ModeOAuth
