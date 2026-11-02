package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/baidubce/bce-cli/internal/config"
	"github.com/spf13/cobra"
)

func TestHasLongLivedKeys(t *testing.T) {
	cases := []struct {
		name    string
		profile config.Profile
		want    bool
	}{
		{"empty", config.Profile{}, false},
		{"access key only", config.Profile{AccessKeyId: "ak"}, true},
		{"secret only", config.Profile{SecretAccessKey: "sk"}, true},
		{"session token only", config.Profile{SecurityToken: "token"}, true},
		{"oauth profile", config.Profile{Mode: config.ModeOAuth, Region: "bj"}, false},
	}
	for _, tc := range cases {
		p := tc.profile
		if got := hasLongLivedKeys(&p); got != tc.want {
			t.Errorf("%s: hasLongLivedKeys() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestResolveAuthProfile(t *testing.T) {
	newCmd := func() (*cobra.Command, *cobra.Command) {
		root := &cobra.Command{Use: "bce"}
		root.PersistentFlags().String("profile", "", "")
		child := &cobra.Command{Use: "login"}
		root.AddCommand(child)
		return root, child
	}

	t.Run("not passed, login follows an OAuth current profile", func(t *testing.T) {
		jsonHome(t)
		cfg := &config.Configuration{
			Current:  "dev",
			Profiles: []config.Profile{{Name: "dev", Mode: config.ModeOAuth}},
		}
		if err := config.Save(cfg); err != nil {
			t.Fatalf("config.Save: %v", err)
		}
		_, child := newCmd()
		if got, err := resolveAuthProfile(child, false); err != nil || got != "dev" {
			t.Errorf("resolveAuthProfile() = %q, %v, want %q", got, err, "dev")
		}
	})

	t.Run("not passed, login ignores an AK current profile", func(t *testing.T) {
		jsonHome(t)
		cfg := &config.Configuration{
			Current:  "work",
			Profiles: []config.Profile{{Name: "work", Mode: config.ModeAK, AccessKeyId: "ak"}},
		}
		if err := config.Save(cfg); err != nil {
			t.Fatalf("config.Save: %v", err)
		}
		_, child := newCmd()
		if got, err := resolveAuthProfile(child, false); err != nil || got != defaultOAuthProfile {
			t.Errorf("resolveAuthProfile() = %q, %v, want %q", got, err, defaultOAuthProfile)
		}
	})

	t.Run("not passed, login", func(t *testing.T) {
		jsonHome(t)
		if err := config.Save(&config.Configuration{Current: "work"}); err != nil {
			t.Fatalf("config.Save: %v", err)
		}
		_, child := newCmd()
		if got, err := resolveAuthProfile(child, false); err != nil || got != defaultOAuthProfile {
			t.Errorf("resolveAuthProfile() = %q, %v, want %q", got, err, defaultOAuthProfile)
		}
	})

	t.Run("not passed, current profile", func(t *testing.T) {
		jsonHome(t)
		if err := config.Save(&config.Configuration{Current: "work"}); err != nil {
			t.Fatalf("config.Save: %v", err)
		}
		_, child := newCmd()
		if got, err := resolveAuthProfile(child, true); err != nil || got != "work" {
			t.Errorf("resolveAuthProfile() = %q, %v, want %q", got, err, "work")
		}
	})

	t.Run("passed", func(t *testing.T) {
		root, child := newCmd()
		if err := root.PersistentFlags().Set("profile", "dev"); err != nil {
			t.Fatalf("Set: %v", err)
		}
		for _, useCurrent := range []bool{false, true} {
			if got, err := resolveAuthProfile(child, useCurrent); err != nil || got != "dev" {
				t.Errorf("resolveAuthProfile(useCurrent=%v) = %q, %v, want %q", useCurrent, got, err, "dev")
			}
		}
	})

	t.Run("invalid current profile name", func(t *testing.T) {
		jsonHome(t)
		if err := config.Save(&config.Configuration{Current: "a.b"}); err != nil {
			t.Fatalf("config.Save: %v", err)
		}
		_, child := newCmd()
		_, err := resolveAuthProfile(child, true)
		if err == nil || !strings.Contains(err.Error(), "current profile") {
			t.Errorf("resolveAuthProfile() error = %v, want current-profile error", err)
		}
	})

	t.Run("name that would share a token file", func(t *testing.T) {
		root, child := newCmd()
		if err := root.PersistentFlags().Set("profile", "a.b"); err != nil {
			t.Fatalf("Set: %v", err)
		}
		if _, err := resolveAuthProfile(child, false); err == nil {
			t.Error("resolveAuthProfile() error = nil, want error")
		}
	})
}

func TestPrintNotLoggedInNamesLoginCommand(t *testing.T) {
	oauthDev := &config.Configuration{
		Current:  "dev",
		Profiles: []config.Profile{{Name: "dev", Mode: config.ModeOAuth}},
	}
	akWork := &config.Configuration{
		Current:  "work",
		Profiles: []config.Profile{{Name: "work", Mode: config.ModeAK, AccessKeyId: "ak"}},
	}
	oauthBadName := &config.Configuration{
		Current:  "a.b",
		Profiles: []config.Profile{{Name: "a.b", Mode: config.ModeOAuth}},
	}
	cases := []struct {
		name    string
		cfg     *config.Configuration
		flag    string // --profile value; "" leaves the flag unset
		profile string // resolved profile passed to printNotLoggedIn
		want    string
		named   bool // whether the notice should name the profile
	}{
		{"current OAuth profile, no --profile", oauthDev, "", "dev", "`bce auth login`", true},
		{"explicit --profile", akWork, "dev", "dev", "`bce auth login --profile dev`", true},
		{"explicit --profile oauth", oauthDev, "oauth", "oauth", "`bce auth login --profile oauth`", true},
		{"current AK profile", akWork, "", "work", "`bce auth login`", false},
		{"current OAuth profile with invalid name", oauthBadName, "", "a.b", "`bce auth login`", false},
		{"no config", nil, "", "default", "`bce auth login`", false},
		{"empty profile", oauthDev, "", "", "`bce auth login`", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			jsonHome(t)
			if tc.cfg != nil {
				if err := config.Save(tc.cfg); err != nil {
					t.Fatalf("config.Save: %v", err)
				}
			}
			root := &cobra.Command{Use: "bce"}
			root.PersistentFlags().String("profile", "", "")
			root.PersistentFlags().Bool("no-color", true, "")
			c := &cobra.Command{Use: "status"}
			root.AddCommand(c)
			if tc.flag != "" {
				if err := root.PersistentFlags().Set("profile", tc.flag); err != nil {
					t.Fatalf("Set: %v", err)
				}
			}
			var buf bytes.Buffer
			printNotLoggedIn(c, &buf, tc.profile)
			got := buf.String()
			if !strings.Contains(got, tc.want) {
				t.Errorf("output %q does not contain %s", got, tc.want)
			}
			if tc.profile != "" {
				if has := strings.Contains(got, `"`+tc.profile+`"`); has != tc.named {
					t.Errorf("output %q names profile %q = %v, want %v", got, tc.profile, has, tc.named)
				}
			}
			if tc.want == "`bce auth login`" && strings.Contains(got, "--profile") {
				t.Errorf("output %q must not name a profile", got)
			}
		})
	}
}

// A bare `bce auth login` while the current profile is a logged-in OAuth
// profile must report that login instead of starting a login for "oauth".
func TestAuthLoginJSONReportsCurrentOAuthProfile(t *testing.T) {
	jsonHome(t)
	seedActiveLogin(t, "dev")
	cfg := &config.Configuration{
		Current:  "dev",
		Profiles: []config.Profile{{Name: "dev", Mode: config.ModeOAuth}},
	}
	if err := config.Save(cfg); err != nil {
		t.Fatalf("config.Save: %v", err)
	}
	login, _, _ := freshAuthCommands()

	out := runAuthJSON(t, login)
	if out["success"] != false || out["authenticated"] != true {
		t.Errorf("output = %v, want already-logged-in report", out)
	}
	if msg, _ := out["message"].(string); !strings.Contains(msg, "Already logged in") {
		t.Errorf("message = %q", msg)
	}
	if loggedIn(defaultOAuthProfile) {
		t.Error("no login must be started for the default oauth profile")
	}
}
