package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ClientID is the public OAuth client registered with IAM for CLI logins.
const ClientID = "bce-cli"

// Scope is the scope set requested at login: openid for the ID Token,
// bce_sts to allow exchanging it for STS credentials, offline_access for
// a Refresh Token so the CLI can stay logged in.
const Scope = "openid bce_sts offline_access"

// oauthBasePath is the IAM path prefix hosting the CLI OAuth 2.0 endpoints.
const oauthBasePath = "/api/iam/cli/oauth2"

// Environment bundles the IAM and STS base addresses used for a login.
type Environment struct {
	IAMBase string
	STSBase string
}

const (
	// DefaultIAMBase hosts the public CLI OAuth endpoints.
	DefaultIAMBase = "https://console.bce.baidu.com"
	// DefaultSTSBase issues the temporary credentials. The ID Token sent there is
	// a bearer credential and the response carries a secret key, so the default
	// is addressed over TLS; pointing --sts-base at a plain HTTP address makes
	// `bce auth login` warn about the plaintext hop.
	DefaultSTSBase = "https://sts.bj.baidubce.com"
)

// ResolveEndpoints determines the IAM and STS base addresses: the built-in
// production addresses unless overridden by explicit arguments.
//
// The addresses are deliberately not configurable through the environment:
// they are only needed for internal testing, and an exported variable would
// silently redirect every later login.
func ResolveEndpoints(iamBase, stsBase string) (Environment, error) {
	e := Environment{IAMBase: DefaultIAMBase, STSBase: DefaultSTSBase}
	if iamBase != "" {
		e.IAMBase = iamBase
	}
	if stsBase != "" {
		e.STSBase = stsBase
	}
	e.IAMBase = strings.TrimRight(e.IAMBase, "/")
	e.STSBase = strings.TrimRight(e.STSBase, "/")
	if e.IAMBase == "" || e.STSBase == "" {
		return Environment{}, fmt.Errorf("both IAM and STS base addresses are required")
	}
	return e, nil
}

// Endpoints holds the OAuth endpoints the CLI talks to.
type Endpoints struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	JwksURI               string `json:"jwks_uri"`
}

// staticEndpoints returns the conventional endpoint layout for an IAM base,
// used before discovery runs and as a fallback when discovery is unavailable.
func staticEndpoints(iamBase string) Endpoints {
	base := strings.TrimRight(iamBase, "/") + oauthBasePath
	return Endpoints{
		AuthorizationEndpoint: base + "/authorize",
		TokenEndpoint:         base + "/token",
		JwksURI:               base + "/.well-known/jwks.json",
	}
}

type cachedEndpoints struct {
	eps       Endpoints
	expiresAt time.Time
}

var (
	discoveryMu    sync.Mutex
	discoveryCache = map[string]cachedEndpoints{}
)

// Discover fetches the OIDC discovery document for an IAM base address.
// Results are cached in memory for the duration advertised by Cache-Control.
// Any failure degrades to the static endpoint layout rather than blocking login.
func Discover(ctx context.Context, client *http.Client, iamBase string) Endpoints {
	iamBase = strings.TrimRight(iamBase, "/")

	discoveryMu.Lock()
	if c, ok := discoveryCache[iamBase]; ok && time.Now().Before(c.expiresAt) {
		discoveryMu.Unlock()
		return c.eps
	}
	discoveryMu.Unlock()

	fallback := staticEndpoints(iamBase)
	url := iamBase + oauthBasePath + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fallback
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return fallback
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fallback
	}
	var eps Endpoints
	if err := json.NewDecoder(resp.Body).Decode(&eps); err != nil {
		return fallback
	}
	if eps.AuthorizationEndpoint == "" || eps.TokenEndpoint == "" {
		return fallback
	}

	if ttl := cacheTTL(resp.Header.Get("Cache-Control")); ttl > 0 {
		discoveryMu.Lock()
		discoveryCache[iamBase] = cachedEndpoints{eps: eps, expiresAt: time.Now().Add(ttl)}
		discoveryMu.Unlock()
	}
	return eps
}

// cacheTTL extracts the max-age directive from a Cache-Control header.
// Returns 0 when absent, unparsable or when the response must not be cached.
func cacheTTL(header string) time.Duration {
	if header == "" {
		return 0
	}
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(strings.ToLower(part))
		if part == "no-store" || part == "no-cache" {
			return 0
		}
		if v, ok := strings.CutPrefix(part, "max-age="); ok {
			secs, err := strconv.Atoi(strings.TrimSpace(v))
			if err != nil || secs <= 0 {
				return 0
			}
			return time.Duration(secs) * time.Second
		}
	}
	return 0
}
