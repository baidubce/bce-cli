package config

import (
	"fmt"
	"os"

	"github.com/baidubce/bce-sdk-go/auth"
)

// BceCredentials is an alias so callers don't need to import bce-sdk-go/auth directly.
type BceCredentials = auth.BceCredentials

// AuthMode identifies which credential strategy the profile uses.
type AuthMode string

const (
	ModeAK       AuthMode = "AK"
	ModeStsToken AuthMode = "StsToken"
)

// Profile holds all credential and preference settings for one named configuration.
type Profile struct {
	Name            string   `json:"name"`
	Mode            AuthMode `json:"mode"`
	AccessKeyId     string   `json:"access_key_id,omitempty"`
	SecretAccessKey string   `json:"secret_access_key,omitempty"`
	SecurityToken   string   `json:"security_token,omitempty"`
	Region          string   `json:"region,omitempty"`
	Endpoint        string   `json:"endpoint,omitempty"`
	Language        string   `json:"language,omitempty"`
}

// FlagOverrides carries per-invocation values from global flags that take
// precedence over the active profile.
type FlagOverrides struct {
	Region        string
	Endpoint      string
	Scheme        string // "http" or "https"; empty means auto-detect from product metadata
	Language      string
	Output        string
	Query         string
	DryRun     bool
	Debug      bool
	NoColor    bool
	Timeout    int  // request timeout in seconds; 0 means use default (15s)
	TotalCount int  // --total-count: cap on total items returned across pages; 0 = unlimited
	Pager      bool // --pager: enable auto-pagination and aggregate all pages
}

// Resolve fills empty profile fields from standard environment variables.
func (p *Profile) Resolve() {
	if p.AccessKeyId == "" {
		p.AccessKeyId = os.Getenv("BCE_ACCESS_KEY_ID")
	}
	if p.SecretAccessKey == "" {
		p.SecretAccessKey = os.Getenv("BCE_SECRET_ACCESS_KEY")
	}
	if p.SecurityToken == "" {
		p.SecurityToken = os.Getenv("BCE_SECURITY_TOKEN")
	}
	if p.Region == "" {
		p.Region = os.Getenv("BCE_REGION")
	}
}

// Credentials returns BCE credentials for the profile's auth mode.
func (p *Profile) Credentials() (*auth.BceCredentials, error) {
	switch p.Mode {
	case ModeAK, "":
		if p.AccessKeyId == "" || p.SecretAccessKey == "" {
			return nil, fmt.Errorf("access_key_id and secret_access_key are required (use `bce configure set` or set BCE_ACCESS_KEY_ID/BCE_SECRET_ACCESS_KEY env vars)")
		}
		return auth.NewBceCredentials(p.AccessKeyId, p.SecretAccessKey)

	case ModeStsToken:
		if p.AccessKeyId == "" || p.SecretAccessKey == "" {
			return nil, fmt.Errorf("access_key_id and secret_access_key are required for StsToken mode (use `bce configure set` or set BCE_ACCESS_KEY_ID/BCE_SECRET_ACCESS_KEY env vars)")
		}
		if p.SecurityToken == "" {
			return nil, fmt.Errorf("security_token is required for StsToken mode")
		}
		return auth.NewSessionBceCredentials(p.AccessKeyId, p.SecretAccessKey, p.SecurityToken)

	default:
		return nil, fmt.Errorf("auth mode %q is not yet supported", p.Mode)
	}
}
