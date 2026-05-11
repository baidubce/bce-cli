package cmd

import (
	"fmt"

	"github.com/baidubce/bce-cli/internal/version"
	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "显示版本信息",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("bce version %s\n", version.Version)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
