// Package sts exchanges an IAM OIDC ID Token for temporary BCE credentials.
package sts

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// DefaultDurationSeconds is the credential lifetime requested by default.
// The value is only a request: STS caps it by its own limit and by the
// remaining validity of the ID Token.
const DefaultDurationSeconds = 3600

// maxResponseBytes caps how much of an STS response is read into memory.
const maxResponseBytes = 1 << 20

// Credentials is a set of STS temporary credentials.
type Credentials struct {
	AccessKeyID     string    `json:"accessKeyId"`
	SecretAccessKey string    `json:"secretAccessKey"`
	SessionToken    string    `json:"sessionToken"`
	Expiration      time.Time `json:"expiration"`
	CreateTime      time.Time `json:"createTime"`
	UserID          string    `json:"userId"`
}

// ExchangeIDToken calls POST {base}/v1/sessionToken/idToken with the ID Token as
// a bearer credential. The returned Expiration is authoritative — it may be
// earlier than durationSeconds implies.
//
// The request carries no body at all: IAM requires the body to be left empty
// when the ACL and attachment extensions are not used, and rejects an empty
// "{}" object.
func ExchangeIDToken(ctx context.Context, client *http.Client, base, idToken string, durationSeconds int) (*Credentials, error) {
	if idToken == "" {
		return nil, fmt.Errorf("id_token is required to obtain STS credentials")
	}
	if durationSeconds <= 0 {
		durationSeconds = DefaultDurationSeconds
	}
	url := sessionTokenURL(base) + "?durationSeconds=" + strconv.Itoa(durationSeconds)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, http.NoBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+idToken)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call STS: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("read STS response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var svcErr struct {
			Code      string `json:"code"`
			Message   string `json:"message"`
			RequestID string `json:"requestId"`
		}
		if json.Unmarshal(body, &svcErr) == nil && svcErr.Message != "" {
			return nil, fmt.Errorf("STS request failed (HTTP %d) [%s] %s (requestId: %s)",
				resp.StatusCode, svcErr.Code, svcErr.Message, svcErr.RequestID)
		}
		return nil, fmt.Errorf("STS request failed with HTTP %d", resp.StatusCode)
	}

	var creds Credentials
	if err := json.Unmarshal(body, &creds); err != nil {
		return nil, fmt.Errorf("parse STS response: %w", err)
	}
	if creds.AccessKeyID == "" || creds.SecretAccessKey == "" || creds.SessionToken == "" {
		return nil, fmt.Errorf("STS response is missing temporary credential fields")
	}
	return &creds, nil
}

// sessionTokenURL joins the STS base with the sessionToken path, tolerating
// bases that already include the /v1 prefix.
func sessionTokenURL(base string) string {
	b := strings.TrimRight(base, "/")
	if strings.HasSuffix(b, "/v1") {
		return b + "/sessionToken/idToken"
	}
	return b + "/v1/sessionToken/idToken"
}
