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
	ModeAK             AuthMode = "AK"
	ModeStsToken       AuthMode = "StsToken"
	ModeAssumeRole     AuthMode = "AssumeRole"
	ModeInstanceRole   AuthMode = "InstanceRole"
	ModeExternal       AuthMode = "External"
	ModeCredentialsURI AuthMode = "CredentialsURI"
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
	OutputFormat    string   `json:"output_format,omitempty"`
}

// FlagOverrides carries per-invocation values from global flags that take
// precedence over the active profile.
type FlagOverrides struct {
	Profile       string
	Region        string
	Endpoint      string
	AccessKeyId   string
	SecretKey     string
	SecurityToken string
	Output        string
	Query         string
	DryRun        bool
	Debug         bool
	NoColor       bool
}

// Resolve fills empty profile fields from standard environment variables.
func (p *Profile) Resolve() {
	if p.AccessKeyId == "" {
		p.AccessKeyId = firstNonEmpty(
			os.Getenv("BCE_ACCESS_KEY_ID"),
			os.Getenv("BAIDUBCE_ACCESS_KEY_ID"),
		)
	}
	if p.SecretAccessKey == "" {
		p.SecretAccessKey = firstNonEmpty(
			os.Getenv("BCE_SECRET_ACCESS_KEY"),
			os.Getenv("BAIDUBCE_SECRET_ACCESS_KEY"),
		)
	}
	if p.SecurityToken == "" {
		p.SecurityToken = os.Getenv("BCE_SECURITY_TOKEN")
	}
	if p.Region == "" {
		p.Region = firstNonEmpty(os.Getenv("BCE_REGION"), "bj")
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
		if p.SecurityToken == "" {
			return nil, fmt.Errorf("security_token is required for StsToken mode")
		}
		return auth.NewSessionBceCredentials(p.AccessKeyId, p.SecretAccessKey, p.SecurityToken)

	default:
		return nil, fmt.Errorf("auth mode %q is not yet supported", p.Mode)
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
