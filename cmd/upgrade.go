package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/baidubce/bce-cli/internal/i18n"
	"github.com/baidubce/bce-cli/internal/updater"
	"github.com/baidubce/bce-cli/internal/version"
	"github.com/spf13/cobra"
)

var upgradeCmd = &cobra.Command{
	GroupID:     "system",
	Use:         "upgrade",
	Short:       i18n.T(i18n.GetLanguage(), "cmd-upgrade"),
	Annotations: map[string]string{"i18n-short": "cmd-upgrade"},
	RunE:        runUpgrade,
}

func init() {
	upgradeCmd.Flags().BoolP("yes", "y", false, i18n.FlagDesc(i18n.GetLanguage(), "upgrade-yes"))
	upgradeCmd.Flags().String("version", "", i18n.FlagDesc(i18n.GetLanguage(), "upgrade-version"))
	rootCmd.AddCommand(upgradeCmd)
}

func runUpgrade(cmd *cobra.Command, args []string) error {
	lang := i18n.GetLanguage()
	yes, _ := cmd.Flags().GetBool("yes")
	targetVer, _ := cmd.Flags().GetString("version")

	if targetVer != "" {
		return runUpgradePinned(lang, targetVer, yes)
	}
	return runUpgradeLatest(lang, yes)
}

func runUpgradeLatest(lang string, yes bool) error {
	fmt.Fprintln(os.Stderr, i18n.T(lang, "upgrade-checking"))

	info, err := updater.CheckLatest()
	if err != nil {
		return fmt.Errorf("check for updates: %w", err)
	}

	current := version.Version
	if updater.CompareVersions(info.Version, current) <= 0 {
		fmt.Fprintf(os.Stderr, i18n.T(lang, "upgrade-up-to-date")+"\n", current)
		return nil
	}

	fmt.Fprintf(os.Stderr, i18n.T(lang, "upgrade-available")+"\n", info.Version, current)

	if !yes && !confirm(i18n.T(lang, "upgrade-confirm")) {
		fmt.Fprintln(os.Stderr, i18n.T(lang, "upgrade-cancelled"))
		return nil
	}

	fmt.Fprintf(os.Stderr, i18n.T(lang, "upgrade-downloading")+"\n", info.Version)

	if err := updater.Install(info.DownloadURL, os.Stderr); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, i18n.T(lang, "upgrade-success")+"\n", info.Version)
	return nil
}

func runUpgradePinned(lang, targetVer string, yes bool) error {
	ver := strings.TrimPrefix(targetVer, "v")
	fmt.Fprintf(os.Stderr, i18n.T(lang, "upgrade-pinned-confirm-msg")+"\n", ver, version.Version)

	if !yes && !confirm(i18n.T(lang, "upgrade-confirm")) {
		fmt.Fprintln(os.Stderr, i18n.T(lang, "upgrade-cancelled"))
		return nil
	}

	fmt.Fprintf(os.Stderr, i18n.T(lang, "upgrade-downloading")+"\n", ver)

	url := updater.DownloadURLForVersion(ver)
	if err := updater.Install(url, os.Stderr); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, i18n.T(lang, "upgrade-success")+"\n", ver)
	return nil
}

// confirm prints prompt and returns true if the user enters y or Y.
func confirm(prompt string) bool {
	fmt.Fprint(os.Stderr, prompt)
	r := bufio.NewReader(os.Stdin)
	line, _ := r.ReadString('\n')
	line = strings.TrimSpace(line)
	return line == "y" || line == "Y"
}
