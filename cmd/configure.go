package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/baidubce/bce-cli/internal/config"
	"github.com/baidubce/bce-cli/internal/i18n"
	"github.com/spf13/cobra"
)

var configureCmd = &cobra.Command{
	GroupID:     "system",
	Use:         "configure",
	Short:       i18n.T(i18n.GetLanguage(), "cmd-configure"),
	Annotations: map[string]string{"i18n-short": "cmd-configure"},
}

var configureSetCmd = &cobra.Command{
	Use:         "set [profile-name]",
	Short:       i18n.T(i18n.GetLanguage(), "cmd-configure-set"),
	Annotations: map[string]string{"i18n-short": "cmd-configure-set"},
	Args:        cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}

		name := ""
		if len(args) > 0 {
			name = args[0]
		}
		if name == "" {
			name = cfg.Current
		}
		if name == "" {
			name = "default"
		}

		p := cfg.GetProfile(name)
		if p == nil {
			p = &config.Profile{Name: name, Mode: config.ModeAK}
		}

		// Apply --mode first so the interactive wizard knows which branch to run.
		// --mode alone does not trigger non-interactive mode.
		if cmd.Flags().Changed("mode") {
			modeStr, _ := cmd.Flags().GetString("mode")
			switch config.AuthMode(modeStr) {
			case config.ModeAK, config.ModeStsToken:
				p.Mode = config.AuthMode(modeStr)
			default:
				return fmt.Errorf("invalid --mode %q: must be AK or StsToken", modeStr)
			}
		}

		// Non-interactive mode: if any credential flag is explicitly provided,
		// apply only the supplied flags and skip stdin prompts entirely.
		// Interactive mode (no flags): prompt for each field as before.
		anyFlagSet := cmd.Flags().Changed("access-key-id") ||
			cmd.Flags().Changed("secret-access-key") ||
			cmd.Flags().Changed("region") ||
			cmd.Flags().Changed("endpoint") ||
			cmd.Flags().Changed("language") ||
			cmd.Flags().Changed("security-token")

		if anyFlagSet {
			if cmd.Flags().Changed("access-key-id") {
				p.AccessKeyId, _ = cmd.Flags().GetString("access-key-id")
			}
			if cmd.Flags().Changed("secret-access-key") {
				p.SecretAccessKey, _ = cmd.Flags().GetString("secret-access-key")
			}
			if cmd.Flags().Changed("region") {
				p.Region, _ = cmd.Flags().GetString("region")
			}
			if cmd.Flags().Changed("endpoint") {
				p.Endpoint, _ = cmd.Flags().GetString("endpoint")
			}
			if cmd.Flags().Changed("language") {
				p.Language, _ = cmd.Flags().GetString("language")
			}
			if cmd.Flags().Changed("security-token") {
				p.SecurityToken, _ = cmd.Flags().GetString("security-token")
			}
		} else {
			r := bufio.NewReader(os.Stdin)
			p.AccessKeyId = prompt(r, "Access Key Id", p.AccessKeyId, false)
			p.SecretAccessKey = prompt(r, "Secret Access Key", p.SecretAccessKey, true)
			p.Region = prompt(r, "Region", p.Region, false)
			if p.Mode == config.ModeStsToken {
				p.SecurityToken = prompt(r, "Security Token", p.SecurityToken, true)
			}
		}

		cfg.SetProfile(*p)
		// Only update the active profile pointer when:
		//   - editing the current profile (name matches), or
		//   - the currently-pointed profile doesn't exist yet (e.g. first-time setup).
		if cfg.Current == name || cfg.GetProfile(cfg.Current) == nil {
			cfg.Current = name
		}
		if err := config.Save(cfg); err != nil {
			return err
		}
		fmt.Printf(i18n.T(i18n.GetLanguage(), "cfg-profile-saved")+"\n", name, config.ConfigDir())
		return nil
	},
}

var configureListCmd = &cobra.Command{
	Use:         "list",
	Short:       i18n.T(i18n.GetLanguage(), "cmd-configure-list"),
	Annotations: map[string]string{"i18n-short": "cmd-configure-list"},
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		lang := i18n.GetLanguage()
		if len(cfg.Profiles) == 0 {
			fmt.Println(i18n.T(lang, "cfg-no-profiles"))
			return nil
		}
		for _, p := range cfg.Profiles {
			cur := "  "
			if p.Name == cfg.Current {
				cur = "* "
			}
			fmt.Printf("%s%-20s [%s: %s]\n", cur, p.Name, i18n.T(lang, "cfg-mode"), p.Mode)
		}
		return nil
	},
}

var configureGetCmd = &cobra.Command{
	Use:         "get [profile-name]",
	Short:       i18n.T(i18n.GetLanguage(), "cmd-configure-get"),
	Annotations: map[string]string{"i18n-short": "cmd-configure-get"},
	Args:        cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		name := ""
		if len(args) > 0 {
			name = args[0]
		}
		p := cfg.GetProfile(name)
		if p == nil {
			if name == "" {
				if cfg.Current != "" {
					return fmt.Errorf("current profile %q not found; run `bce configure set` to recreate it", cfg.Current)
				}
				return fmt.Errorf("no profile configured yet; run `bce configure set` to get started")
			}
			return fmt.Errorf("profile %q not found", name)
		}
		lang := i18n.GetLanguage()
		fmt.Printf("%-16s  %s\n", i18n.T(lang, "cfg-field-name")+":", p.Name)
		fmt.Printf("%-16s  %s\n", i18n.T(lang, "cfg-field-mode")+":", p.Mode)
		fmt.Printf("%-16s  %s\n", "AccessKeyId:", maskSecret(p.AccessKeyId, 4))
		fmt.Printf("%-16s  %s\n", "SecretAccessKey:", maskSecret(p.SecretAccessKey, 0))
		if p.Region != "" {
			fmt.Printf("%-16s  %s\n", i18n.T(lang, "cfg-field-region")+":", p.Region)
		}
		if p.Language != "" {
			fmt.Printf("%-16s  %s\n", i18n.T(lang, "cfg-field-language")+":", p.Language)
		}
		if p.Endpoint != "" {
			fmt.Printf("%-16s  %s\n", i18n.T(lang, "cfg-field-endpoint")+":", p.Endpoint)
		}
		if p.Mode == config.ModeStsToken {
			fmt.Printf("%-16s  %s\n", "SecurityToken:", maskSecret(p.SecurityToken, 0))
		}
		return nil
	},
}

var configureUseCmd = &cobra.Command{
	Use:         "use <profile-name>",
	Short:       i18n.T(i18n.GetLanguage(), "cmd-configure-use"),
	Annotations: map[string]string{"i18n-short": "cmd-configure-use"},
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		name := args[0]
		if cfg.GetProfile(name) == nil {
			return fmt.Errorf("%s", fmt.Sprintf(i18n.T(i18n.GetLanguage(), "cfg-not-found"), name))
		}
		cfg.Current = name
		if err := config.Save(cfg); err != nil {
			return err
		}
		fmt.Printf(i18n.T(i18n.GetLanguage(), "cfg-switched")+"\n", name)
		return nil
	},
}

var configureDeleteCmd = &cobra.Command{
	Use:         "delete <profile-name>",
	Short:       i18n.T(i18n.GetLanguage(), "cmd-configure-delete"),
	Annotations: map[string]string{"i18n-short": "cmd-configure-delete"},
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		name := args[0]
		before := len(cfg.Profiles)
		var kept []config.Profile
		for _, p := range cfg.Profiles {
			if p.Name != name {
				kept = append(kept, p)
			}
		}
		if len(kept) == before {
			return fmt.Errorf("%s", fmt.Sprintf(i18n.T(i18n.GetLanguage(), "cfg-not-found"), name))
		}

		yes, _ := cmd.Flags().GetBool("yes")
		if !yes {
			fmt.Fprintf(os.Stderr, i18n.T(i18n.GetLanguage(), "cfg-delete-confirm")+"\n", name)
			r := bufio.NewReader(os.Stdin)
			line, _ := r.ReadString('\n')
			line = strings.TrimSpace(line)
			if line != "y" && line != "Y" {
				fmt.Fprintln(os.Stderr, i18n.T(i18n.GetLanguage(), "upgrade-cancelled"))
				return nil
			}
		}

		cfg.Profiles = kept
		if cfg.Current == name {
			if len(kept) > 0 {
				cfg.Current = kept[0].Name
				fmt.Fprintf(os.Stderr, "switched current profile to %q\n", cfg.Current)
			} else {
				cfg.Current = ""
			}
		}
		if err := config.Save(cfg); err != nil {
			return err
		}
		fmt.Printf(i18n.T(i18n.GetLanguage(), "cfg-profile-deleted")+"\n", name)
		return nil
	},
}

func init() {
	configureSetCmd.Flags().String("access-key-id", "", "Access Key ID")
	configureSetCmd.Flags().String("secret-access-key", "", "Secret Access Key [SENSITIVE: prefer interactive mode to avoid shell history]")
	configureSetCmd.Flags().String("security-token", "", "STS security token [SENSITIVE: prefer interactive mode to avoid shell history]")
	configureSetCmd.Flags().String("region", "", "Region (e.g. bj / gz / su)")
	configureSetCmd.Flags().String("endpoint", "", "Service endpoint override")
	configureSetCmd.Flags().String("language", "", "Display language (zh-CN / en-US)")
	configureSetCmd.Flags().String("mode", "", "Auth mode (AK / StsToken)")
	configureDeleteCmd.Flags().BoolP("yes", "y", false, i18n.FlagDesc(i18n.GetLanguage(), "cfg-delete-yes"))
	configureCmd.AddCommand(
		configureSetCmd,
		configureListCmd,
		configureGetCmd,
		configureUseCmd,
		configureDeleteCmd,
	)
	rootCmd.AddCommand(configureCmd)
}

func prompt(r *bufio.Reader, label, current string, secret bool) string {
	if current != "" {
		if secret {
			fmt.Printf("%s [****]: ", label)
		} else {
			fmt.Printf("%s [%s]: ", label, current)
		}
	} else {
		fmt.Printf("%s: ", label)
	}
	line, err := r.ReadString('\n')
	line = strings.TrimSpace(line)
	if err != nil || line == "" {
		return current
	}
	return line
}

func maskSecret(s string, visible int) string {
	if len(s) <= visible {
		return strings.Repeat("*", len(s))
	}
	return s[:visible] + strings.Repeat("*", len(s)-visible)
}
