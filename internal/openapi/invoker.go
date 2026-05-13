package openapi

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/baidubce/bce-cli/internal/config"
	"github.com/baidubce/bce-cli/internal/meta"
	"github.com/baidubce/bce-sdk-go/auth"
	"github.com/baidubce/bce-sdk-go/bce"
)

// Invoker executes a single BCE API call.
type Invoker struct {
	profile   *config.Profile
	api       *meta.Api
	params    map[string]string
	scheme    string // "http" or "https"
	endpoint  string // hostname only, e.g. "bcc.bj.baidubce.com"
	overrides *config.FlagOverrides
	timeout   int // request timeout in seconds; 0 means default (15s)
}

// NewInvoker creates an Invoker, resolving the endpoint from overrides > profile > product metadata.
func NewInvoker(
	profile *config.Profile,
	api *meta.Api,
	params map[string]string,
	overrides *config.FlagOverrides,
	product *meta.Product,
) (*Invoker, error) {
	endpoint, err := resolveEndpoint(profile, overrides, product)
	if err != nil {
		return nil, err
	}
	scheme, err := resolveScheme(product, overrides.Scheme)
	if err != nil {
		return nil, err
	}
	return &Invoker{
		profile:   profile,
		api:       api,
		params:    params,
		scheme:    scheme,
		endpoint:  endpoint,
		overrides: overrides,
		timeout:   overrides.Timeout,
	}, nil
}

// resolveScheme determines the protocol to use.
// If the user specified --scheme, validate it against the protocols listed in
// product.Protocol (pipe-separated, e.g. "HTTP|HTTPS"). Returns an error when
// the requested scheme is not supported by the product.
// When no override is given, the first protocol in the list is used; defaults to "https".
func resolveScheme(product *meta.Product, override string) (string, error) {
	// Parse supported schemes from product metadata.
	supported := parseSupportedSchemes(product.Protocol)

	if override != "" {
		lower := strings.ToLower(override)
		if lower != "http" && lower != "https" {
			return "", fmt.Errorf("invalid --scheme %q: must be http or https", override)
		}
		if len(supported) > 0 && !containsScheme(supported, lower) {
			return "", fmt.Errorf(
				"scheme %q is not supported for service %q (supported: %s)",
				lower, product.Code, strings.Join(supported, ", "),
			)
		}
		return lower, nil
	}

	// Default: prefer https; fall back to first listed protocol if https is not supported.
	if len(supported) == 0 || containsScheme(supported, "https") {
		return "https", nil
	}
	return supported[0], nil
}

// parseSupportedSchemes splits a protocol string like "HTTP|HTTPS" into
// lowercase scheme names ["http", "https"].
func parseSupportedSchemes(protocol string) []string {
	if protocol == "" {
		return nil
	}
	parts := strings.Split(protocol, "|")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, strings.ToLower(strings.TrimSpace(p)))
	}
	return out
}

func containsScheme(schemes []string, s string) bool {
	for _, v := range schemes {
		if v == s {
			return true
		}
	}
	return false
}

func resolveEndpoint(p *config.Profile, ov *config.FlagOverrides, product *meta.Product) (string, error) {
	if ov.Endpoint != "" {
		return ov.Endpoint, nil
	}
	if p.Endpoint != "" {
		return p.Endpoint, nil
	}
	region := ov.Region
	if region == "" {
		region = p.Region
	}
	if region == "" {
		return "", fmt.Errorf("region is required for service %q: use --region or set it in your profile (e.g. bce configure set --region bj)", product.Code)
	}
	if ep, ok := product.Endpoint[region]; ok {
		return ep, nil
	}
	supported := make([]string, 0, len(product.Endpoint))
	for r := range product.Endpoint {
		supported = append(supported, r)
	}
	sort.Strings(supported)
	return "", fmt.Errorf("region %q is not supported for service %q (supported: %s)", region, product.Code, strings.Join(supported, ", "))
}

// Call executes the API request and returns the parsed JSON response.
func (inv *Invoker) Call() (map[string]interface{}, error) {
	// Apply parameter defaults for values not provided by the user.
	params := make(map[string]string, len(inv.params))
	for k, v := range inv.params {
		params[k] = v
	}
	for _, p := range inv.api.Parameters {
		if _, ok := params[p.Name]; !ok && p.Default != nil {
			params[p.Name] = defaultToString(p.Default)
		}
	}

	// Build URI: replace {version} and {paramName} path variables
	uri := buildURI(inv.api.URI, inv.api.Parameters, params)

	// Split any fixed query params embedded in the URI template (e.g. "?authorizeRule")
	// so they are passed via SetParam rather than left in the path where the SDK would
	// percent-encode the '?' into '%3F'.
	fixedQueryParams := make(map[string]string)
	if idx := strings.IndexByte(uri, '?'); idx >= 0 {
		for _, part := range strings.Split(uri[idx+1:], "&") {
			if k, v, ok := strings.Cut(part, "="); ok {
				fixedQueryParams[k] = v
			} else if part != "" {
				fixedQueryParams[part] = ""
			}
		}
		uri = uri[:idx]
	}

	// Distribute params by position (needed for both dry-run and real request)
	queryParams := make(map[string]string)
	for k, v := range fixedQueryParams {
		queryParams[k] = v
	}
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
		fmt.Fprintf(os.Stderr, "[DRY-RUN] %s %s://%s%s\n", inv.api.Method, inv.scheme, inv.endpoint, uri)
		if len(queryParams) > 0 {
			parts := make([]string, 0, len(queryParams))
			for k, v := range queryParams {
				parts = append(parts, k+"="+v)
			}
			fmt.Fprintf(os.Stderr, "[DRY-RUN] Query: %s\n", strings.Join(parts, "&"))
		}
		if len(bodyParams) > 0 {
			raw, _ := json.MarshalIndent(bodyParams, "", "  ")
			fmt.Fprintf(os.Stderr, "[DRY-RUN] Body:\n%s\n", raw)
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

	timeoutMs := inv.timeout * 1000
	if timeoutMs <= 0 {
		timeoutMs = 15 * 1000
	}

	conf := &bce.BceClientConfiguration{
		Endpoint:    sdkEndpoint,
		Credentials: creds,
		SignOption: &auth.SignOptions{
			HeadersToSign: auth.DEFAULT_HEADERS_TO_SIGN,
			ExpireSeconds: auth.DEFAULT_EXPIRE_SECONDS,
		},
		Retry:                     bce.DEFAULT_RETRY_POLICY,
		ConnectionTimeoutInMillis: timeoutMs,
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
		fmt.Fprintf(os.Stderr, "[DEBUG] > %s %s://%s%s\n", req.Method(), inv.scheme, inv.endpoint, req.Uri())
		if len(queryParams) > 0 {
			parts := make([]string, 0, len(queryParams))
			for k, v := range queryParams {
				parts = append(parts, k+"="+v)
			}
			fmt.Fprintf(os.Stderr, "[DEBUG] > Query: %s\n", strings.Join(parts, "&"))
		}
		for k, v := range headerParams {
			fmt.Fprintf(os.Stderr, "[DEBUG] > Header: %s: %s\n", k, v)
		}
		if len(bodyParams) > 0 {
			fmt.Fprintf(os.Stderr, "[DEBUG] > Header: Content-Type: application/json;charset=utf-8\n")
			raw, _ := json.MarshalIndent(bodyParams, "", "  ")
			fmt.Fprintf(os.Stderr, "[DEBUG] > Body:\n%s\n", raw)
		}
	}

	resp := &bce.BceResponse{}
	if err := client.SendRequest(req, resp); err != nil {
		return nil, formatError(err)
	}
	defer resp.Body().Close()

	if inv.overrides.Debug {
		fmt.Fprintf(os.Stderr, "[DEBUG] < Status: %d\n", resp.StatusCode())
		for k, v := range resp.Headers() {
			fmt.Fprintf(os.Stderr, "[DEBUG] < Header: %s: %s\n", k, v)
		}
	}

	if resp.IsFail() {
		svcErr := resp.ServiceError()
		if svcErr != nil {
			return nil, fmt.Errorf("[%s] %s (requestId: %s)", svcErr.Code, svcErr.Message, svcErr.RequestId)
		}
		return nil, fmt.Errorf("request failed with status %d", resp.StatusCode())
	}

	// 204 No Content and similar empty responses have no JSON body.
	if resp.StatusCode() == 204 {
		return nil, nil
	}

	var result map[string]interface{}
	if err := resp.ParseJsonBody(&result); err != nil {
		s := err.Error()
		if strings.Contains(s, "EOF") || strings.Contains(s, "unexpected end of JSON input") {
			return nil, nil
		}
		return nil, fmt.Errorf("parse response: %w", err)
	}

	if inv.overrides.Debug && result != nil {
		raw, _ := json.MarshalIndent(result, "", "  ")
		fmt.Fprintf(os.Stderr, "[DEBUG] < Body:\n%s\n", raw)
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
	case "integer", "int", "long":
		if n, err := strconv.ParseInt(val, 10, 64); err == nil {
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

// defaultToString converts a schema default value (unmarshaled from JSON as any)
// to the string form used in the params map.
// JSON numbers decode as float64; booleans as bool; strings stay as-is.
func defaultToString(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case bool:
		if val {
			return "true"
		}
		return "false"
	case float64:
		if val == float64(int64(val)) {
			return strconv.FormatInt(int64(val), 10)
		}
		return strconv.FormatFloat(val, 'f', -1, 64)
	default:
		return fmt.Sprintf("%v", v)
	}
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
