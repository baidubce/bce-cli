package cmd

import (
	"fmt"
	"runtime"

	"github.com/baidubce/bce-cli/internal/i18n"
	"github.com/baidubce/bce-cli/internal/version"
	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	GroupID:     "system",
	Use:         "version",
	Short:       i18n.T(i18n.GetLanguage(), "cmd-version"),
	Annotations: map[string]string{"i18n-short": "cmd-version"},
	Run: func(cmd *cobra.Command, args []string) {
		verbose, _ := cmd.Flags().GetBool("verbose")
		if verbose {
			fmt.Printf("bce version %s\n", version.Version)
			fmt.Printf("  commit:     %s\n", version.Commit)
			fmt.Printf("  built:      %s\n", version.BuildTime)
			fmt.Printf("  go version: %s\n", runtime.Version())
			fmt.Printf("  platform:   %s/%s\n", runtime.GOOS, runtime.GOARCH)
		} else {
			fmt.Println(version.Version)
		}
	},
}

func init() {
	versionCmd.Flags().BoolP("verbose", "v", false, "show full build information")
	rootCmd.AddCommand(versionCmd)
}
