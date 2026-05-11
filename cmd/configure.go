package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/baidubce/bce-cli/internal/config"
	"github.com/spf13/cobra"
)

var configureCmd = &cobra.Command{
	Use:   "configure",
	Short: "管理凭证和配置 profile",
}

var configureSetCmd = &cobra.Command{
	Use:   "set",
	Short: "交互式配置当前 profile（AK/SK/Region）",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}

		name, _ := cmd.Flags().GetString("profile")
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

		r := bufio.NewReader(os.Stdin)
		p.AccessKeyId = prompt(r, "Access Key Id", p.AccessKeyId, false)
		p.SecretAccessKey = prompt(r, "Secret Access Key", p.SecretAccessKey, true)
		p.Region = prompt(r, "Region (bj/gz/su/bd)", p.Region, false)

		cfg.SetProfile(*p)
		cfg.Current = name
		if err := config.Save(cfg); err != nil {
			return err
		}
		fmt.Printf("Profile %q saved to %s\n", name, config.ConfigDir())
		return nil
	},
}

var configureListCmd = &cobra.Command{
	Use:   "list",
	Short: "列出所有 profile",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		if len(cfg.Profiles) == 0 {
			fmt.Println("No profiles configured. Run `bce configure set` to create one.")
			return nil
		}
		for _, p := range cfg.Profiles {
			cur := "  "
			if p.Name == cfg.Current {
				cur = "* "
			}
			fmt.Printf("%s%-20s [mode: %-15s region: %s]\n", cur, p.Name, p.Mode, p.Region)
		}
		return nil
	},
}

var configureGetCmd = &cobra.Command{
	Use:   "get",
	Short: "查看指定 profile 的配置详情",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		name, _ := cmd.Flags().GetString("profile")
		p := cfg.GetProfile(name)
		if p == nil {
			return fmt.Errorf("profile %q not found", name)
		}
		fmt.Printf("Name:             %s\n", p.Name)
		fmt.Printf("Mode:             %s\n", p.Mode)
		fmt.Printf("AccessKeyId:      %s\n", maskSecret(p.AccessKeyId, 4))
		fmt.Printf("SecretAccessKey:  %s\n", maskSecret(p.SecretAccessKey, 0))
		fmt.Printf("Region:           %s\n", p.Region)
		if p.Endpoint != "" {
			fmt.Printf("Endpoint:         %s\n", p.Endpoint)
		}
		return nil
	},
}

var configureDeleteCmd = &cobra.Command{
	Use:   "delete <profile-name>",
	Short: "删除指定 profile",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		name := args[0]
		before := len(cfg.Profiles)
		kept := cfg.Profiles[:0]
		for _, p := range cfg.Profiles {
			if p.Name != name {
				kept = append(kept, p)
			}
		}
		if len(kept) == before {
			return fmt.Errorf("profile %q not found", name)
		}
		cfg.Profiles = kept
		if cfg.Current == name {
			cfg.Current = ""
		}
		if err := config.Save(cfg); err != nil {
			return err
		}
		fmt.Printf("Profile %q deleted.\n", name)
		return nil
	},
}

func init() {
	configureSetCmd.Flags().String("profile", "", "profile 名称（默认使用当前 profile）")
	configureGetCmd.Flags().String("profile", "", "profile 名称（默认使用当前 profile）")

	configureCmd.AddCommand(
		configureSetCmd,
		configureListCmd,
		configureGetCmd,
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
	line, _ := r.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
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
