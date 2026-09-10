package cmd

import (
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

func TestRejectProfileFlag(t *testing.T) {
	newCmd := func() (*cobra.Command, *cobra.Command) {
		root := &cobra.Command{Use: "bce"}
		root.PersistentFlags().String("profile", "", "")
		child := &cobra.Command{Use: "login"}
		root.AddCommand(child)
		return root, child
	}

	t.Run("not passed", func(t *testing.T) {
		_, child := newCmd()
		if err := rejectProfileFlag(child); err != nil {
			t.Errorf("rejectProfileFlag() = %v, want nil", err)
		}
	})

	t.Run("passed", func(t *testing.T) {
		root, child := newCmd()
		if err := root.PersistentFlags().Set("profile", "foo"); err != nil {
			t.Fatalf("Set: %v", err)
		}
		if err := rejectProfileFlag(child); err == nil {
			t.Error("rejectProfileFlag() = nil, want error")
		}
	})
}
