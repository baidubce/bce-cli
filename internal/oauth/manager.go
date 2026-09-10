package oauth

import (
	"context"
	"crypto/hmac"
	"fmt"
	"net/http"
	"time"

	"github.com/baidubce/bce-cli/internal/sts"
)

const (
	// stsRefreshWindow is the safety margin before STS expiry at which the CLI
	// proactively obtains fresh credentials.
	stsRefreshWindow = 5 * time.Minute
	// idTokenSkew is the margin before ID Token expiry at which it is considered
	// too old to still be accepted by STS.
	idTokenSkew = 60 * time.Second

	defaultHTTPTimeout = 30 * time.Second

	// CredentialsTimeout bounds a full credential renewal: discovery, refresh and
	// the STS exchange run sequentially, each bounded by defaultHTTPTimeout.
	CredentialsTimeout = 3*defaultHTTPTimeout + 10*time.Second
)

// Manager owns the OAuth login state of one profile: the browser login flow,
// refresh-token rotation and STS credential renewal.
type Manager struct {
	Profile         string
	HTTP            *http.Client
	DurationSeconds int
}

// NewManager returns a Manager for a profile using a default HTTP client.
func NewManager(profile string) *Manager {
	return &Manager{
		Profile:         profile,
		HTTP:            newHTTPClient(defaultHTTPTimeout),
		DurationSeconds: sts.DefaultDurationSeconds,
	}
}

// LoginOptions configures one browser login.
type LoginOptions struct {
	Env Environment
	// OnAuthorizationURL is invoked with the authorization URL once the loopback
	// listener is bound, before waiting for the callback. The caller is
	// responsible for opening or displaying it.
	OnAuthorizationURL func(url string)
	// Timeout bounds the wait for the browser callback; 0 uses the IAM
	// authorization transaction lifetime.
	Timeout time.Duration
}

// Login performs the Authorization Code + PKCE flow, exchanges the resulting ID
// Token for STS credentials, and persists both.
func (m *Manager) Login(ctx context.Context, opts LoginOptions) (*TokenSet, error) {
	pkce, err := NewPKCE()
	if err != nil {
		return nil, err
	}
	state, err := NewStateValue()
	if err != nil {
		return nil, err
	}
	nonce, err := NewStateValue()
	if err != nil {
		return nil, err
	}

	// Bind the loopback port first so redirect_uri always names a port we own.
	server, err := NewCallbackServer(state)
	if err != nil {
		return nil, err
	}
	defer server.Close()

	client := &Client{HTTP: m.HTTP, Endpoints: Discover(ctx, m.HTTP, opts.Env.IAMBase)}
	authURL := client.AuthorizationURL(server.RedirectURI(), state, nonce, pkce.Challenge)
	if opts.OnAuthorizationURL != nil {
		opts.OnAuthorizationURL(authURL)
	}

	code, err := server.Wait(ctx, opts.Timeout)
	if err != nil {
		return nil, err
	}
	// The authorization code is single-use and short-lived: exchange immediately.
	tr, err := client.ExchangeCode(ctx, code, server.RedirectURI(), pkce.Verifier)
	if err != nil {
		return nil, err
	}
	// Bind the freshly issued ID Token to this login request. Only the initial
	// exchange is checked, and only when IAM echoes the nonce back: refreshed ID
	// Tokens never carry one, so a missing nonce cannot be treated as an error.
	// PKCE remains the primary defence against code interception.
	if claims, err := DecodeIDToken(tr.IDToken); err == nil && claims.Nonce != "" &&
		!hmac.Equal([]byte(claims.Nonce), []byte(nonce)) {
		return nil, fmt.Errorf("id_token nonce does not match the login request; aborting")
	}

	ts := &TokenSet{
		IAMBase: opts.Env.IAMBase,
		STSBase: opts.Env.STSBase,
	}
	ts.ApplyTokenResponse(tr, time.Now())

	creds, err := sts.ExchangeIDToken(ctx, m.HTTP, ts.STSBase, ts.IDToken, m.durationSeconds())
	if err != nil {
		return nil, err
	}
	ts.STS = creds

	if err := withLock(ctx, m.Profile, func() error { return SaveTokens(m.Profile, ts) }); err != nil {
		return nil, err
	}
	return ts, nil
}

// Logout discards the local login state of the profile.
func (m *Manager) Logout(ctx context.Context) error {
	return withLock(ctx, m.Profile, func() error { return DeleteTokens(m.Profile) })
}

// Status returns the persisted login state without renewing anything.
func (m *Manager) Status() (*TokenSet, error) {
	return LoadTokens(m.Profile)
}

// Credentials returns usable STS credentials, renewing them when needed:
//
//  1. cached STS credentials that are still comfortably valid are reused;
//  2. otherwise a still-valid ID Token is exchanged for new STS credentials;
//  3. otherwise the Refresh Token is used to obtain a new ID Token first.
//
// An invalid_grant response clears the whole local login state, since IAM
// rotates refresh tokens and revokes the token family on replay.
func (m *Manager) Credentials(ctx context.Context) (*sts.Credentials, error) {
	// Fast path: no lock needed when the cached credentials are still good.
	if ts, err := LoadTokens(m.Profile); err == nil && stsUsable(ts.STS) {
		return ts.STS, nil
	}

	var creds *sts.Credentials
	err := withLock(ctx, m.Profile, func() error {
		ts, err := LoadTokens(m.Profile)
		if err != nil {
			return err
		}
		// Another process may have renewed while we waited for the lock.
		if stsUsable(ts.STS) {
			creds = ts.STS
			return nil
		}
		if ts.STSBase == "" || ts.IAMBase == "" {
			return fmt.Errorf("OAuth login state is incomplete; run `bce auth login` again")
		}

		if !idTokenUsable(ts) {
			if err := m.refreshLocked(ctx, ts); err != nil {
				return err
			}
		}

		c, err := sts.ExchangeIDToken(ctx, m.HTTP, ts.STSBase, ts.IDToken, m.durationSeconds())
		if err != nil {
			return err
		}
		ts.STS = c
		if err := SaveTokens(m.Profile, ts); err != nil {
			return err
		}
		creds = c
		return nil
	})
	if err != nil {
		return nil, err
	}
	return creds, nil
}

// refreshLocked rotates the refresh token in place. It must be called while
// holding the profile lock.
func (m *Manager) refreshLocked(ctx context.Context, ts *TokenSet) error {
	if ts.RefreshToken == "" {
		return fmt.Errorf("OAuth session expired and no refresh token is available; run `bce auth login`")
	}
	if !ts.RefreshTokenExpiresAt.IsZero() && !time.Now().Before(ts.RefreshTokenExpiresAt) {
		_ = DeleteTokens(m.Profile)
		return fmt.Errorf("OAuth refresh token expired; run `bce auth login`")
	}

	client := &Client{HTTP: m.HTTP, Endpoints: Discover(ctx, m.HTTP, ts.IAMBase)}
	tr, err := client.Refresh(ctx, ts.RefreshToken)
	if err != nil {
		if IsInvalidGrant(err) {
			// The token family is gone; keeping the old token around would only
			// trigger replay detection on the next attempt.
			_ = DeleteTokens(m.Profile)
			return fmt.Errorf("OAuth session is no longer valid (%v); run `bce auth login`", err)
		}
		return err
	}
	ts.ApplyTokenResponse(tr, time.Now())
	// Persist the rotated refresh token before it is used any further: the old
	// one is already dead server-side. The caller writes again after the STS
	// exchange; the extra write is deliberate, not redundant.
	return SaveTokens(m.Profile, ts)
}

func (m *Manager) durationSeconds() int {
	if m.DurationSeconds > 0 {
		return m.DurationSeconds
	}
	return sts.DefaultDurationSeconds
}

// stsUsable reports whether cached STS credentials are complete and still valid
// beyond the refresh safety window.
//
// STS may issue credentials whose whole lifetime is shorter than the window (it
// caps them by the remaining ID Token validity). Renewing on every command in
// that case would add a round trip per invocation, so short-lived credentials
// are instead reused until half their lifetime has elapsed.
func stsUsable(c *sts.Credentials) bool {
	if c == nil || c.AccessKeyID == "" || c.SecretAccessKey == "" || c.SessionToken == "" {
		return false
	}
	if c.Expiration.IsZero() {
		return false
	}
	now := time.Now()
	if now.Add(stsRefreshWindow).Before(c.Expiration) {
		return true
	}
	if !c.CreateTime.IsZero() && c.Expiration.After(c.CreateTime) {
		lifetime := c.Expiration.Sub(c.CreateTime)
		if lifetime <= 2*stsRefreshWindow {
			return now.Before(c.Expiration.Add(-lifetime / 2))
		}
	}
	return false
}

// idTokenUsable reports whether the stored ID Token can still be exchanged.
func idTokenUsable(ts *TokenSet) bool {
	if ts.IDToken == "" {
		return false
	}
	if ts.IDTokenExpiresAt.IsZero() {
		return false
	}
	return time.Now().Add(idTokenSkew).Before(ts.IDTokenExpiresAt)
}
