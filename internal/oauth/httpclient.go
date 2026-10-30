package oauth

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

// insecureTLSEnv opts out of certificate verification for the IAM and STS calls
// made by this package. It exists for test deployments served by a private CA;
// there is deliberately no CLI flag, because token refresh and STS renewal
// happen on later invocations where a login flag would not be available.
const insecureTLSEnv = "BCE_INSECURE_SKIP_TLS_VERIFY"

// InsecureTLS reports whether certificate verification is disabled by
// $BCE_INSECURE_SKIP_TLS_VERIFY. Anything other than a truthy boolean value
// (true, 1, t, T, TRUE...) leaves verification enabled.
func InsecureTLS() bool {
	v := os.Getenv(insecureTLSEnv)
	if v == "" {
		return false
	}
	enabled, err := strconv.ParseBool(v)
	return err == nil && enabled
}

var insecureWarnOnce sync.Once

// newHTTPClient builds the HTTP client used for IAM discovery, the token
// endpoint and the STS exchange.
//
// Only those requests are affected: BCE API calls go through the SDK's own
// client and always verify certificates.
func newHTTPClient(timeout time.Duration) *http.Client {
	client := &http.Client{Timeout: timeout}
	if !InsecureTLS() {
		return client
	}
	// Clone the default transport so proxy settings and connection pooling are
	// preserved; only certificate verification is turned off.
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if tr.TLSClientConfig == nil {
		tr.TLSClientConfig = &tls.Config{}
	}
	tr.TLSClientConfig.InsecureSkipVerify = true
	client.Transport = tr

	insecureWarnOnce.Do(func() {
		fmt.Fprintf(os.Stderr,
			"warning: %s is set; TLS certificates of the IAM and STS endpoints are not verified\n",
			insecureTLSEnv)
	})
	return client
}
