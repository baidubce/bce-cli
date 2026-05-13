package cmd

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/baidubce/bce-cli/internal/config"
	"github.com/baidubce/bce-cli/internal/i18n"
	"github.com/baidubce/bce-cli/internal/openapi"
	"github.com/baidubce/bce-cli/internal/suggester"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:                "bce",
	Short:              i18n.T(i18n.GetLanguage(), "cmd-root-short"),
	Long:               i18n.T(i18n.GetLanguage(), "cmd-root-long"),
	SilenceUsage:       true,
	SilenceErrors:      true,
	DisableSuggestions: true,
	Annotations: map[string]string{
		"i18n-short": "cmd-root-short",
		"i18n-long":  "cmd-root-long",
	},
}

// Execute is the entry point called from main.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		rootCmd.PrintErrln("Error:", err)
		if hint := suggestionsForError(err.Error()); hint != "" {
			rootCmd.PrintErr(hint)
		}
		os.Exit(1)
	}
}

var unknownCmdRe = regexp.MustCompile(`unknown command "([^"]+)" for "([^"]+)"`)

// suggestionsForError parses cobra's "unknown command" errors and returns a
// formatted hint string listing the closest matching subcommands.
func suggestionsForError(msg string) string {
	m := unknownCmdRe.FindStringSubmatch(msg)
	if m == nil {
		return ""
	}
	input, parentPath := m[1], m[2]

	// Walk the cobra tree to find the parent command.
	parent := rootCmd
	for _, part := range strings.Fields(parentPath)[1:] { // skip root "bce"
		for _, sub := range parent.Commands() {
			if sub.Name() == part {
				parent = sub
				break
			}
		}
	}

	var names []string
	for _, c := range parent.Commands() {
		if !c.Hidden {
			names = append(names, c.Name())
		}
	}

	suggestions := suggester.SuggestCommand(input, names)
	if len(suggestions) == 0 {
		return ""
	}

	var sb strings.Builder
	fmt.Fprint(&sb, i18n.T(i18n.GetLanguage(), "suggest-hint"))
	for _, s := range suggestions {
		fmt.Fprintf(&sb, "  %s %s\n", parentPath, s)
	}
	sb.WriteByte('\n')
	return sb.String()
}

func init() {
	// SetHelpFunc intercepts cobra-rendered help so --language is applied before
	// flag descriptions are printed.
	defaultHelp := rootCmd.HelpFunc()
	rootCmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		if l, _ := cmd.Root().PersistentFlags().GetString("language"); l != "" {
			if err := i18n.SetLanguage(l); err != nil {
				fmt.Fprintln(os.Stderr, "Error:", err)
				return
			}
		}
		lang := i18n.GetLanguage()

		// Refresh flag descriptions — covers both root persistent flags and local
		// flags on any subcommand (e.g. --yes on upgrade/delete, --version on upgrade).
		var refreshFlagDescs func(*cobra.Command)
		refreshFlagDescs = func(c *cobra.Command) {
			for _, name := range []string{
				"profile", "region", "endpoint", "scheme", "language", "output", "query",
				"unfold", "dry-run", "debug", "no-color", "timeout",
				"upgrade-yes", "upgrade-version", "cfg-delete-yes",
			} {
				if f := c.Flags().Lookup(name); f != nil {
					if desc := i18n.FlagDesc(lang, name); desc != "" {
						f.Usage = desc
					}
				}
			}
		}
		refreshFlagDescs(cmd)
		for _, sub := range cmd.Commands() {
			refreshFlagDescs(sub)
		}

		// Refresh Short/Long for static commands (those with i18n annotations).
		var refreshStatic func(*cobra.Command)
		refreshStatic = func(c *cobra.Command) {
			if key, ok := c.Annotations["i18n-short"]; ok {
				c.Short = i18n.T(lang, key)
			}
			if key, ok := c.Annotations["i18n-long"]; ok {
				c.Long = i18n.T(lang, key)
			}
		}
		refreshStatic(cmd)
		for _, sub := range cmd.Commands() {
			refreshStatic(sub)
			// Cobra's auto-generated help command has no i18n annotation — set it by name.
			if sub.Name() == "help" {
				if desc := i18n.T(lang, "cmd-help"); desc != "" {
					sub.Short = desc
				}
			}
		}

		// Refresh Short for dynamically-registered service commands.
		openapi.RefreshServiceDescs(cmd.Root())

		defaultHelp(cmd, args)
	})

	// PersistentPreRunE runs before every non-action command (configure, version, etc.)
	// and ensures the language is set from the --language flag when provided.
	// Action commands use DisableFlagParsing so they handle --language via their own pre-scan.
	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if l, _ := cmd.Root().PersistentFlags().GetString("language"); l != "" {
			return i18n.SetLanguage(l)
		}
		// --language not passed; apply profile.Language if configured.
		profileName, _ := cmd.Root().PersistentFlags().GetString("profile")
		if cfg, err := config.Load(); err == nil {
			if p := cfg.GetProfile(profileName); p != nil && p.Language != "" {
				_ = i18n.SetLanguage(p.Language) // profile language is trusted
			}
		}
		return nil
	}

	// Global persistent flags available to every command
	lang := i18n.GetLanguage()
	pf := rootCmd.PersistentFlags()
	pf.String("profile", "", i18n.FlagDesc(lang, "profile"))
	pf.String("region", "", i18n.FlagDesc(lang, "region"))
	pf.String("endpoint", "", i18n.FlagDesc(lang, "endpoint"))
	pf.String("scheme", "", i18n.FlagDesc(lang, "scheme"))
	pf.String("language", "", i18n.FlagDesc(lang, "language"))
	pf.String("output", "json", i18n.FlagDesc(lang, "output"))
	pf.String("query", "", i18n.FlagDesc(lang, "query"))
	pf.Bool("dry-run", false, i18n.FlagDesc(lang, "dry-run"))
	pf.Bool("debug", false, i18n.FlagDesc(lang, "debug"))
	pf.Bool("no-color", false, i18n.FlagDesc(lang, "no-color"))
	pf.Bool("unfold", false, i18n.FlagDesc(lang, "unfold"))
	pf.Int("timeout", 0, i18n.FlagDesc(lang, "timeout"))

	// Dynamically register one cobra.Command per service from products.json
	openapi.RegisterCommands(rootCmd)

	// Group system commands under "Commands:" and put cobra's
	// auto-generated help command in the same group.
	rootCmd.AddGroup(&cobra.Group{ID: "system", Title: "Commands:"})
	rootCmd.SetHelpCommandGroupID("system")

	// Custom usage template: root shows [service|command], subcommands show [command].
	rootCmd.SetUsageTemplate(`Usage:{{if and .Runnable (not .HasAvailableSubCommands)}}
  {{if index .Annotations "action"}}{{.CommandPath}} [--parameter value ...]{{else}}{{.UseLine}}{{end}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} {{if .HasParent}}{{if index .Annotations "service"}}<operation> [--parameter value ...]{{else}}[command]{{end}}{{else}}<service> <operation> [--parameter value ...]{{end}}{{end}}{{if gt (len .Aliases) 0}}

Aliases:
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

Examples:
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{if eq (len .Groups) 0}}

{{if .HasParent}}Available Operations:{{else}}Available Commands:{{end}}{{range $cmds}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{else}}{{range $group := .Groups}}

{{.Title}}{{range $cmds}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if not .AllChildCommandsHaveGroup}}

Additional Commands:{{range $cmds}}{{if (and (eq .GroupID "") (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

Flags:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

Global Flags:
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasHelpSubCommands}}

Additional help topics:{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{rpad .CommandPath .CommandPathPadding}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

Use "{{.CommandPath}} {{if .HasParent}}{{if index .Annotations "service"}}<operation>{{else}}[command]{{end}}{{else}}<service> <operation>{{end}} --help" for more information about a command.
{{end}}`)
}
