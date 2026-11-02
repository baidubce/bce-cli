package cmd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/baidubce/bce-cli/internal/oauth"
	"github.com/spf13/cobra"
)

// execLogoutAll executes `logout --all --format json` with the given stdin and
// extra args, returning the parsed stdout and the command error.
func execLogoutAll(t *testing.T, stdin string, extraArgs ...string) (map[string]any, error) {
	t.Helper()
	root := &cobra.Command{Use: "bce", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().String("profile", "", "")
	root.PersistentFlags().Bool("no-color", false, "")
	logout := &cobra.Command{Use: "logout", RunE: authLogoutCmd.RunE}
	logout.Flags().String("format", "text", "")
	logout.Flags().Bool("all", false, "")
	logout.Flags().BoolP("yes", "y", false, "")
	root.AddCommand(logout)

	buf := captureOut(logout)
	root.SetIn(strings.NewReader(stdin))
	root.SetErr(&strings.Builder{})
	root.SetArgs(append([]string{"logout", "--all", "--format", "json"}, extraArgs...))
	err := root.Execute()

	var parsed map[string]any
	if buf.Len() > 0 {
		if jerr := json.Unmarshal(buf.Bytes(), &parsed); jerr != nil {
			t.Fatalf("output is not valid JSON: %v\n%s", jerr, buf.String())
		}
	}
	return parsed, err
}

func loggedIn(profile string) bool {
	_, err := oauth.NewManager(profile).Status()
	return err == nil
}

func TestAuthLogoutAllYesClearsEveryLogin(t *testing.T) {
	jsonHome(t)
	seedActiveLogin(t, defaultOAuthProfile)
	seedActiveLogin(t, "dev")

	out, err := execLogoutAll(t, "", "--yes")
	if err != nil {
		t.Fatalf("logout --all --yes: %v", err)
	}
	if out["success"] != true || out["authenticated"] != false {
		t.Errorf("output = %v", out)
	}
	if loggedIn(defaultOAuthProfile) || loggedIn("dev") {
		t.Error("all logins must be removed")
	}
}

func TestAuthLogoutAllInteractiveConfirmations(t *testing.T) {
	cases := []struct {
		name    string
		stdin   string
		cleared bool
	}{
		{"confirmed", "y\n", true},
		{"yes is not y", "yes\n", false},
		{"declined", "n\n", false},
		{"empty stdin", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			jsonHome(t)
			seedActiveLogin(t, defaultOAuthProfile)
			seedActiveLogin(t, "dev")

			out, err := execLogoutAll(t, tc.stdin)
			if err != nil {
				t.Fatalf("logout --all: %v", err)
			}
			if tc.cleared {
				if out["success"] != true || loggedIn(defaultOAuthProfile) || loggedIn("dev") {
					t.Errorf("logins must be cleared: %v", out)
				}
				return
			}
			if out["success"] != false || out["message"] != "Cancelled" {
				t.Errorf("output = %v, want cancelled", out)
			}
			if !loggedIn(defaultOAuthProfile) || !loggedIn("dev") {
				t.Error("cancelled logout must keep every login")
			}
		})
	}
}

func TestAuthLogoutAllNothingToClear(t *testing.T) {
	jsonHome(t)
	out, err := execLogoutAll(t, "")
	if err != nil {
		t.Fatalf("logout --all: %v", err)
	}
	if out["success"] != true || out["message"] != "Not logged in" {
		t.Errorf("output = %v", out)
	}
}

func TestAuthLogoutAllRejectsProfile(t *testing.T) {
	jsonHome(t)
	seedActiveLogin(t, "dev")
	out, err := execLogoutAll(t, "", "--yes", "--profile", "dev")
	if err != nil {
		t.Fatalf("json mode reports the conflict in the payload, got error: %v", err)
	}
	if msg, _ := out["message"].(string); out["success"] != false || !strings.Contains(msg, "--profile") {
		t.Errorf("output = %v, want success=false mentioning --profile", out)
	}
	if !loggedIn("dev") {
		t.Error("login must be untouched when --all is rejected")
	}
}
