package openapi

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/baidubce/bce-sdk-go/auth"
	"github.com/baidubce/bce-sdk-go/bce"
	"github.com/baidubce/bce-cli/internal/config"
	"github.com/baidubce/bce-cli/internal/meta"
)

// Invoker executes a single BCE API call.
type Invoker struct {
	profile   *config.Profile
	api       *meta.Api
	params    map[string]string
	scheme    string // "http" or "https"
	endpoint  string // hostname only, e.g. "bcc.bj.baidubce.com"
	overrides *config.FlagOverrides
}

// NewInvoker creates an Invoker, resolving the endpoint from overrides > profile > product metadata.
func NewInvoker(
	profile *config.Profile,
	api *meta.Api,
	params map[string]string,
	overrides *config.FlagOverrides,
	product *meta.Product,
) *Invoker {
	return &Invoker{
		profile:   profile,
		api:       api,
		params:    params,
		scheme:    resolveScheme(product),
		endpoint:  resolveEndpoint(profile, overrides, product),
		overrides: overrides,
	}
}

// resolveScheme picks the first protocol listed in product.Protocol.
// Defaults to "https" when the field is empty.
func resolveScheme(product *meta.Product) string {
	if product.Protocol == "" {
		return "https"
	}
	first := strings.SplitN(product.Protocol, "|", 2)[0]
	return strings.ToLower(strings.TrimSpace(first))
}

func resolveEndpoint(p *config.Profile, ov *config.FlagOverrides, product *meta.Product) string {
	if ov.Endpoint != "" {
		return ov.Endpoint
	}
	if p.Endpoint != "" {
		return p.Endpoint
	}
	region := ov.Region
	if region == "" {
		region = p.Region
	}
	if region == "" {
		// no region specified — use the "default" endpoint
		if ep, ok := product.Endpoint["default"]; ok {
			return ep
		}
	} else {
		if ep, ok := product.Endpoint[region]; ok {
			return ep
		}
		// region specified but not found — fall back to "default"
		if ep, ok := product.Endpoint["default"]; ok {
			return ep
		}
	}
	return ""
}

// Call executes the API request and returns the parsed JSON response.
func (inv *Invoker) Call() (map[string]interface{}, error) {
	// Apply parameter defaults for values not provided by the user.
	params := make(map[string]string, len(inv.params))
	for k, v := range inv.params {
		params[k] = v
	}
	for _, p := range inv.api.Parameters {
		if _, ok := params[p.Name]; !ok && p.Default != "" {
			params[p.Name] = p.Default
		}
	}

	// Build URI: replace {version} and {paramName} path variables
	uri := buildURI(inv.api.URI, inv.api.Parameters, params)

	// Distribute params by position (needed for both dry-run and real request)
	queryParams := make(map[string]string)
	headerParams := make(map[string]string)
	bodyParams := make(map[string]interface{})
	for _, p := range inv.api.Parameters {
		val, ok := params[p.Name]
		if !ok {
			continue
		}
		switch strings.ToUpper(p.Position) {
		case "QUERY":
			queryParams[p.Name] = val
		case "HEADER":
			headerParams[p.Name] = val
		case "BODY":
			bodyParams[p.Name] = coerceValue(p, val)
		case "URL":
			// already substituted in buildURI
		}
	}

	if inv.overrides.DryRun {
		fmt.Printf("[DRY-RUN] %s %s://%s%s\n", inv.api.Method, inv.scheme, inv.endpoint, uri)
		if len(queryParams) > 0 {
			parts := make([]string, 0, len(queryParams))
			for k, v := range queryParams {
				parts = append(parts, k+"="+v)
			}
			fmt.Printf("[DRY-RUN] Query: %s\n", strings.Join(parts, "&"))
		}
		if len(bodyParams) > 0 {
			raw, _ := json.MarshalIndent(bodyParams, "", "  ")
			fmt.Printf("[DRY-RUN] Body:\n%s\n", raw)
		}
		return nil, nil
	}

	creds, err := inv.profile.Credentials()
	if err != nil {
		return nil, err
	}

	// BCE SDK treats endpoints without a scheme as HTTPS by default.
	// Prefix with "http://" explicitly when HTTP is required.
	sdkEndpoint := inv.endpoint
	if inv.scheme == "http" {
		sdkEndpoint = "http://" + inv.endpoint
	}

	conf := &bce.BceClientConfiguration{
		Endpoint:    sdkEndpoint,
		Credentials: creds,
		SignOption: &auth.SignOptions{
			HeadersToSign: auth.DEFAULT_HEADERS_TO_SIGN,
			ExpireSeconds: auth.DEFAULT_EXPIRE_SECONDS,
		},
		Retry:                     bce.DEFAULT_RETRY_POLICY,
		ConnectionTimeoutInMillis: 30 * 1000,
	}
	client := bce.NewBceClient(conf, &auth.BceV1Signer{})

	req := &bce.BceRequest{}
	req.SetMethod(inv.api.Method)
	req.SetUri(uri)

	for k, v := range queryParams {
		req.SetParam(k, v)
	}
	for k, v := range headerParams {
		req.SetHeader(k, v)
	}

	if len(bodyParams) > 0 {
		bodyBytes, err := json.Marshal(bodyParams)
		if err != nil {
			return nil, fmt.Errorf("marshal request body: %w", err)
		}
		body, err := bce.NewBodyFromBytes(bodyBytes)
		if err != nil {
			return nil, fmt.Errorf("create request body: %w", err)
		}
		req.SetBody(body)
		req.SetHeader("Content-Type", "application/json;charset=utf-8")
	}

	if inv.overrides.Debug {
		fmt.Printf("[DEBUG] %s %s://%s%s\n", req.Method(), inv.scheme, inv.endpoint, req.Uri())
	}

	resp := &bce.BceResponse{}
	if err := client.SendRequest(req, resp); err != nil {
		return nil, formatError(err)
	}
	defer resp.Body().Close()

	if resp.IsFail() {
		svcErr := resp.ServiceError()
		if svcErr != nil {
			return nil, fmt.Errorf("[%s] %s (requestId: %s)", svcErr.Code, svcErr.Message, svcErr.RequestId)
		}
		return nil, fmt.Errorf("request failed with status %d", resp.StatusCode())
	}

	var result map[string]interface{}
	if err := resp.ParseJsonBody(&result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return result, nil
}

// buildURI replaces URL-position parameter placeholders in the URI template.
func buildURI(uriTemplate string, params []meta.Parameter, values map[string]string) string {
	uri := uriTemplate
	for _, p := range params {
		if strings.ToUpper(p.Position) == "URL" {
			if val, ok := values[p.Name]; ok {
				uri = strings.ReplaceAll(uri, "{"+p.Name+"}", val)
			}
		}
	}
	return uri
}

// coerceValue converts a string flag value to the appropriate Go type based on parameter type.
func coerceValue(p meta.Parameter, val string) interface{} {
	switch strings.ToLower(p.Type) {
	case "integer":
		var n int64
		if _, err := fmt.Sscanf(val, "%d", &n); err == nil {
			return n
		}
	case "boolean":
		return strings.ToLower(val) == "true"
	case "list", "object":
		var v interface{}
		if err := json.Unmarshal([]byte(val), &v); err == nil {
			return v
		}
	}
	return val
}

func formatError(err error) error {
	switch e := err.(type) {
	case *bce.BceServiceError:
		return fmt.Errorf("[%s] %s (requestId: %s)", e.Code, e.Message, e.RequestId)
	case *bce.BceClientError:
		return fmt.Errorf("client error: %s", e.Error())
	}
	return err
}
