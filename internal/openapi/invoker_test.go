package openapi

import (
	"strings"
	"testing"

	"github.com/baidubce/bce-cli/internal/config"
	"github.com/baidubce/bce-cli/internal/meta"
)

func TestResolveEndpointMissingRegion(t *testing.T) {
	product := &meta.Product{Code: "VPC", Endpoint: map[string]string{
		"gz": "bcc.gz.baidubce.com",
		"bj": "bcc.bj.baidubce.com",
	}}
	_, err := resolveEndpoint(&config.Profile{}, &config.FlagOverrides{}, product)
	if err == nil {
		t.Fatal("expected error when region is not set")
	}
	for _, want := range []string{"region is required", "supported: bj, gz", "BCE_REGION"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}

	ep, err := resolveEndpoint(&config.Profile{Region: "gz"}, &config.FlagOverrides{}, product)
	if err != nil || ep != "bcc.gz.baidubce.com" {
		t.Errorf("got %q, %v", ep, err)
	}

	if _, err := resolveEndpoint(&config.Profile{Region: "su"}, &config.FlagOverrides{}, product); err == nil ||
		!strings.Contains(err.Error(), `region "su" is not supported`) {
		t.Errorf("expected unsupported-region error, got %v", err)
	}
}

func TestResolveEndpointGlobalFallback(t *testing.T) {
	product := &meta.Product{Code: "PRIVATEZONE", Endpoint: map[string]string{
		"global": "privatezone.baidubce.com",
	}}
	cases := []struct {
		name    string
		profile *config.Profile
		ov      *config.FlagOverrides
	}{
		{"profile region", &config.Profile{Region: "bj"}, &config.FlagOverrides{}},
		{"no region", &config.Profile{}, &config.FlagOverrides{}},
		{"explicit --region", &config.Profile{Region: "bj"}, &config.FlagOverrides{Region: "gz"}},
		{"explicit global", &config.Profile{}, &config.FlagOverrides{Region: "global"}},
	}
	for _, c := range cases {
		ep, err := resolveEndpoint(c.profile, c.ov, product)
		if err != nil || ep != "privatezone.baidubce.com" {
			t.Errorf("%s: got %q, %v", c.name, ep, err)
		}
	}

	// A mixed product keeps using a dedicated regional endpoint when one exists.
	product.Endpoint["bj"] = "privatezone.bj.baidubce.com"
	if ep, _ := resolveEndpoint(&config.Profile{Region: "bj"}, &config.FlagOverrides{}, product); ep != "privatezone.bj.baidubce.com" {
		t.Errorf("mixed product: got %q", ep)
	}
	if ep, _ := resolveEndpoint(&config.Profile{Region: "gz"}, &config.FlagOverrides{}, product); ep != "privatezone.baidubce.com" {
		t.Errorf("mixed product fallback: got %q", ep)
	}
}
