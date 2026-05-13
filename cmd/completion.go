package cmd

import (
	"os"

	"github.com/baidubce/bce-cli/internal/i18n"
	"github.com/spf13/cobra"
)

var completionCmd = &cobra.Command{
	GroupID:     "system",
	Use:         "completion [bash|zsh|fish|powershell]",
	Short:       i18n.T(i18n.GetLanguage(), "cmd-completion"),
	Annotations: map[string]string{"i18n-short": "cmd-completion"},
	ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
	Args:      cobra.ExactValidArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		switch args[0] {
		case "bash":
			return rootCmd.GenBashCompletion(os.Stdout)
		case "zsh":
			return rootCmd.GenZshCompletion(os.Stdout)
		case "fish":
			return rootCmd.GenFishCompletion(os.Stdout, true)
		case "powershell":
			return rootCmd.GenPowerShellCompletionWithDesc(os.Stdout)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(completionCmd)
}
