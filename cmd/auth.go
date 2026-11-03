package cmd

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/baidubce/bce-cli/internal/config"
	"github.com/baidubce/bce-cli/internal/i18n"
	"github.com/baidubce/bce-cli/internal/oauth"
	"github.com/spf13/cobra"
)

// defaultOAuthProfile is the profile `bce auth login` uses when --profile is
// not given and the current profile is not an OAuth profile (see
// resolveAuthProfile).
const defaultOAuthProfile = oauth.DefaultProfile

// defaultLoginRegion is written to a profile on `auth login` when neither
// --region nor an existing profile region is set.
const defaultLoginRegion = "bj"

var authCmd = &cobra.Command{
	GroupID:     "system",
	Use:         "auth",
	Short:       i18n.T(i18n.GetLanguage(), "cmd-auth"),
	Annotations: map[string]string{"i18n-short": "cmd-auth"},
}

var authLoginCmd = &cobra.Command{
	Use:         "login",
	Short:       i18n.T(i18n.GetLanguage(), "cmd-auth-login"),
	Annotations: map[string]string{"i18n-short": "cmd-auth-login"},
	Args:        cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		format, err := authFormat(cmd)
		if err != nil {
			return err
		}
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		name, err := resolveAuthProfile(cmd, false)
		if err != nil {
			return err
		}
		lang := i18n.GetLanguage()

		// Report an existing session of this profile instead of starting a
		// second browser flow. ActiveLogin only looks at the named profile, so
		// a login under another profile never blocks this one. This guard only
		// applies while the profile is also the current one — if the user has
		// switched current elsewhere (e.g. back to an AK profile), a fresh
		// login proceeds and simply replaces the old token file on success;
		// a failed attempt leaves the previous login untouched.
		activeTokens := oauth.ActiveLogin(name)
		if activeTokens != nil && cfg.Current == name {
			if format == "json" {
				identity := identityOf(activeTokens).Identity()
				message := fmt.Sprintf("Already logged in as %s. To re-login, run: `bce auth logout` first", identity)
				fields := []jsonField{
					{"success", false},
					{"authenticated", true},
					{"message", message},
				}
				// A zero deadline can mean either "already expired" or "the
				// Refresh Token carries no expiry claim"; distinguish them
				// instead of reporting a misleading "<1m" for a login that in
				// fact never expires.
				deadline := sessionDeadline(activeTokens)
				if !deadline.IsZero() {
					fields = append(fields,
						jsonField{"token_expires", formatExpiry(deadline)},
						jsonField{"token_remaining", humanizeDuration(time.Until(deadline))})
				} else if activeTokens.RefreshToken != "" && activeTokens.RefreshTokenExpiresAt.IsZero() {
					fields = append(fields, jsonField{"token_remaining", "never"})
				}
				return writeAuthJSON(cmd, fields)
			}
			printAlreadyAuthenticated(cmd, activeTokens, "bce auth logout")
			return nil
		}

		p := cfg.GetProfile(name)
		if p == nil {
			p = &config.Profile{Name: name}
		} else if p.Mode != config.ModeOAuth && hasLongLivedKeys(p) {
			// Converting an AK/StsToken profile discards its keys; never do that
			// silently.
			fmt.Fprintf(cmd.ErrOrStderr(), i18n.T(lang, "auth-profile-converted")+"\n", name)
		}

		iamBase, _ := cmd.Flags().GetString("iam-base")
		stsBase, _ := cmd.Flags().GetString("sts-base")
		env, err := oauth.ResolveEndpoints(iamBase, stsBase)
		if err != nil {
			return err
		}

		noBrowser, _ := cmd.Flags().GetBool("no-browser")
		timeoutSecs, _ := cmd.Flags().GetInt("callback-timeout")
		out := cmd.OutOrStdout()

		mgr := oauth.NewManager(name)
		// The ID Token is a bearer credential and STS returns a secret key: warn
		// when either hop is not TLS-protected.
		for _, base := range []string{env.IAMBase, env.STSBase} {
			if !strings.HasPrefix(strings.ToLower(base), "https://") {
				fmt.Fprintf(cmd.ErrOrStderr(), i18n.T(lang, "auth-insecure-base")+"\n", base)
			}
		}
		ts, loginErr := mgr.Login(cmd.Context(), oauth.LoginOptions{
			Env:     env,
			Timeout: time.Duration(timeoutSecs) * time.Second,
			OnAuthorizationURL: func(url string) {
				if !noBrowser {
					if err := oauth.OpenBrowser(url); err != nil {
						fmt.Fprintf(cmd.ErrOrStderr(), "warning: %v\n", err)
					}
				}
				// The authorization URL and progress notice are diagnostic, not
				// data, so they go to stderr in both formats: --format json
				// callers only parse stdout, and text-mode users still see them
				// interleaved with the rest of the command's stderr output.
				fmt.Fprintf(cmd.ErrOrStderr(), "%s\n%s\n", i18n.T(lang, "auth-open-hint"), url)
				fmt.Fprintln(cmd.ErrOrStderr(), i18n.T(lang, "auth-waiting"))
			},
		})
		if loginErr != nil {
			if format == "json" {
				return writeAuthJSON(cmd, []jsonField{
					{"success", false},
					{"authenticated", false},
					{"message", oauth.ClassifyAuthorizationError(loginErr)},
				})
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "%s %v\n", failMark(authStyled(cmd, cmd.ErrOrStderr())), loginErr)
			return errSilent
		}

		// Persist the profile only after a successful login so a failed attempt
		// never leaves a profile pointing at an OAuth mode with no tokens.
		p.Mode = config.ModeOAuth
		// Long-lived keys must not linger in config.json once the profile relies
		// on browser-issued temporary credentials.
		clearLongLivedKeys(p)
		if region, _ := cmd.Root().PersistentFlags().GetString("region"); region != "" {
			p.Region = region
		} else if p.Region == "" && os.Getenv("BCE_REGION") == "" {
			// Leave the region empty when BCE_REGION is set so the env var
			// keeps taking effect at runtime.
			p.Region = defaultLoginRegion
		}
		cfg.SetProfile(*p)
		// Point the CLI at the profile just authenticated; `bce configure use`
		// switches back.
		switched := cfg.Current != name
		cfg.Current = name
		if err := config.Save(cfg); err != nil {
			return err
		}

		if format == "json" {
			fields := append([]jsonField{{"success", true}, {"authenticated", true}}, authStatusJSONFields(name, ts)...)
			return writeAuthJSON(cmd, fields)
		}

		fmt.Fprintf(out, "%s %s\n", okMark(authStyled(cmd, out)), i18n.T(lang, "auth-login-success"))
		if switched {
			fmt.Fprintf(out, i18n.T(lang, "auth-current-switched")+"\n", name)
		}
		printAuthStatus(cmd, ts)
		return nil
	},
}

var authStatusCmd = &cobra.Command{
	Use:         "status",
	Short:       i18n.T(i18n.GetLanguage(), "cmd-auth-status"),
	Annotations: map[string]string{"i18n-short": "cmd-auth-status"},
	Args:        cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		format, err := authFormat(cmd)
		if err != nil {
			return err
		}
		name, err := resolveAuthProfile(cmd, true)
		if err != nil {
			return err
		}
		ts, err := oauth.NewManager(name).Status()
		if err != nil {
			if err == oauth.ErrNotLoggedIn {
				if format == "json" {
					return writeAuthJSON(cmd, []jsonField{
						{"authenticated", false},
						{"message", "Not logged in"},
					})
				}
				out := cmd.OutOrStdout()
				printNotLoggedIn(cmd, out, name)
				return nil
			}
			return err
		}
		if format == "json" {
			return writeLoggedInJSON(cmd, name, ts, nil)
		}
		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "%s %s\n", okMark(authStyled(cmd, out)), i18n.T(i18n.GetLanguage(), "auth-authenticated"))
		printNotCurrentNotice(cmd, out, name)
		fmt.Fprintf(out, "%-22s  %s\n", i18n.T(i18n.GetLanguage(), "auth-field-profile")+":", name)
		printAuthStatus(cmd, ts)
		return nil
	},
}

var authRefreshCmd = &cobra.Command{
	Use:         "refresh",
	Short:       i18n.T(i18n.GetLanguage(), "cmd-auth-refresh"),
	Annotations: map[string]string{"i18n-short": "cmd-auth-refresh"},
	Args:        cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		format, err := authFormat(cmd)
		if err != nil {
			return err
		}
		name, err := resolveAuthProfile(cmd, true)
		if err != nil {
			return err
		}
		mgr := oauth.NewManager(name)

		if _, err := mgr.Status(); err == oauth.ErrNotLoggedIn {
			if format == "json" {
				return writeAuthJSON(cmd, []jsonField{
					{"success", false},
					{"authenticated", false},
					{"message", "Not logged in"},
				})
			}
			out := cmd.OutOrStdout()
			printNotLoggedIn(cmd, out, name)
			return nil
		}

		// Ensure usable credentials via the same three-tier logic the API path
		// uses: reuse still-valid STS credentials, else exchange a still-valid
		// ID Token, else rotate the refresh token first. A dead refresh token
		// clears the local state. When the cached credentials are still within
		// their safety window this reuses them without a network call — hence
		// the "ready" wording below rather than claiming a renewal always
		// happened.
		ctx, cancel := context.WithTimeout(cmd.Context(), oauth.CredentialsTimeout)
		defer cancel()
		if _, err := mgr.Credentials(ctx); err != nil {
			if format == "json" {
				return writeAuthJSON(cmd, []jsonField{
					{"success", false},
					{"authenticated", false},
					{"message", err.Error()},
				})
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "%s %v\n", failMark(authStyled(cmd, cmd.ErrOrStderr())), err)
			return errSilent
		}

		// Credentials only returns the STS credentials; reload the full token set
		// so the (possibly renewed) expiries can be shown.
		ts, err := mgr.Status()
		if err != nil {
			return err
		}
		if format == "json" {
			return writeLoggedInJSON(cmd, name, ts, []jsonField{{"success", true}})
		}
		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "%s %s\n", okMark(authStyled(cmd, out)), i18n.T(i18n.GetLanguage(), "auth-refreshed"))
		printNotCurrentNotice(cmd, out, name)
		fmt.Fprintf(out, "%-22s  %s\n", i18n.T(i18n.GetLanguage(), "auth-field-profile")+":", name)
		printAuthStatus(cmd, ts)
		return nil
	},
}

// writeLoggedInJSON renders the shared logged-in payload (authenticated + the
// session detail fields, plus current_profile when it differs) used by both
// `status` and `refresh`. prefix carries any command-specific leading fields
// (e.g. success for refresh) placed before "authenticated".
func writeLoggedInJSON(cmd *cobra.Command, profile string, ts *oauth.TokenSet, prefix []jsonField) error {
	fields := append(append([]jsonField{}, prefix...), jsonField{"authenticated", true})
	fields = append(fields, authStatusJSONFields(profile, ts)...)
	if cur, err := currentProfileName(); err == nil && cur != profile {
		fields = append(fields, jsonField{"current_profile", cur})
	}
	return writeAuthJSON(cmd, fields)
}

// printNotCurrentNotice warns, in text mode, when the current profile is not
// the oauth login being reported — the reported credentials are not the ones
// subsequent API calls would actually use until the user switches.
func printNotCurrentNotice(cmd *cobra.Command, out io.Writer, profile string) {
	if cur, err := currentProfileName(); err == nil && cur != profile {
		fmt.Fprintf(out, "%s %s\n", warnMark(authStyled(cmd, out)), fmt.Sprintf(i18n.T(i18n.GetLanguage(), "auth-not-current"), cur, profile))
	}
}

var authLogoutCmd = &cobra.Command{
	Use:         "logout",
	Short:       i18n.T(i18n.GetLanguage(), "cmd-auth-logout"),
	Annotations: map[string]string{"i18n-short": "cmd-auth-logout"},
	Args:        cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		format, err := authFormat(cmd)
		if err != nil {
			return err
		}
		if all, _ := cmd.Flags().GetBool("all"); all {
			return runLogoutAll(cmd, format)
		}
		name, err := resolveAuthProfile(cmd, true)
		if err != nil {
			return err
		}
		mgr := oauth.NewManager(name)

		// This check and the Logout call below are not atomic together, but
		// DeleteTokens is a no-op when the file is already gone, so the only
		// effect of losing this race is a "Not logged in" message for a login
		// that existed a moment earlier — never a crash or a stuck lock.
		if _, err := mgr.Status(); err == oauth.ErrNotLoggedIn {
			if format == "json" {
				return writeAuthJSON(cmd, []jsonField{
					{"success", true},
					{"authenticated", false},
					{"message", "Not logged in"},
				})
			}
			out := cmd.OutOrStdout()
			printNotLoggedIn(cmd, out, name)
			return nil
		}

		if err := mgr.Logout(cmd.Context()); err != nil {
			return err
		}
		if format == "json" {
			return writeAuthJSON(cmd, []jsonField{
				{"success", true},
				{"authenticated", false},
				{"message", "Logged out"},
			})
		}
		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "%s %s\n", okMark(authStyled(cmd, out)), i18n.T(i18n.GetLanguage(), "auth-logged-out"))
		return nil
	},
}

// runLogoutAll clears every OAuth login on disk. Unless --yes is given it asks
// once (y/N, like `configure delete`); anything but an explicit y cancels, so a
// non-interactive stdin without --yes never deletes anything. config.json is
// left untouched, as with a single-profile logout.
func runLogoutAll(cmd *cobra.Command, format string) error {
	lang := i18n.GetLanguage()
	if cmd.Root().PersistentFlags().Changed("profile") {
		if format == "json" {
			return writeAuthJSON(cmd, []jsonField{
				{"success", false},
				{"message", "--all cannot be combined with --profile"},
			})
		}
		return fmt.Errorf("%s", i18n.T(lang, "auth-all-no-profile"))
	}
	names, err := oauth.ListProfiles()
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if len(names) == 0 {
		if format == "json" {
			return writeAuthJSON(cmd, []jsonField{
				{"success", true},
				{"authenticated", false},
				{"message", "Not logged in"},
			})
		}
		printNotLoggedIn(cmd, out, "")
		return nil
	}

	if yes, _ := cmd.Flags().GetBool("yes"); !yes {
		errw := cmd.ErrOrStderr()
		fmt.Fprintf(errw, i18n.T(lang, "auth-logout-all-list")+"\n", len(names))
		for _, n := range names {
			fmt.Fprintf(errw, "  - %s\n", n)
		}
		r := bufio.NewReader(cmd.InOrStdin())
		fmt.Fprint(errw, i18n.T(lang, "auth-logout-all-ask"))
		line, _ := r.ReadString('\n')
		if a := strings.TrimSpace(line); a != "y" && a != "Y" {
			return logoutAllCancelled(cmd, format)
		}
	}

	var failed []string
	for _, n := range names {
		if err := oauth.NewManager(n).Logout(cmd.Context()); err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", n, err))
		}
	}
	if len(failed) > 0 {
		detail := strings.Join(failed, "; ")
		if format == "json" {
			return writeAuthJSON(cmd, []jsonField{
				{"success", false},
				{"message", fmt.Sprintf("failed to log out of %d of %d profiles: %s", len(failed), len(names), detail)},
			})
		}
		msg := fmt.Sprintf(i18n.T(lang, "auth-logout-all-failed"), len(failed), len(names), detail)
		fmt.Fprintf(cmd.ErrOrStderr(), "%s %s\n", failMark(authStyled(cmd, cmd.ErrOrStderr())), msg)
		return errSilent
	}
	if format == "json" {
		return writeAuthJSON(cmd, []jsonField{
			{"success", true},
			{"authenticated", false},
			{"message", "Logged out"},
			{"profiles", names},
		})
	}
	fmt.Fprintf(out, "%s %s\n", okMark(authStyled(cmd, out)), fmt.Sprintf(i18n.T(lang, "auth-logged-out-all"), len(names)))
	return nil
}

func logoutAllCancelled(cmd *cobra.Command, format string) error {
	if format == "json" {
		return writeAuthJSON(cmd, []jsonField{
			{"success", false},
			{"message", "Cancelled"},
		})
	}
	// "upgrade-cancelled" is a generic "Cancelled." message shared on purpose.
	fmt.Fprintln(cmd.ErrOrStderr(), i18n.T(i18n.GetLanguage(), "upgrade-cancelled"))
	return nil
}

// printAuthStatus prints non-sensitive details of a login. Tokens and secret
// keys are never printed.
func printAuthStatus(cmd *cobra.Command, ts *oauth.TokenSet) {
	lang := i18n.GetLanguage()
	w := cmd.OutOrStdout()
	line := func(key, value string) {
		if value != "" {
			fmt.Fprintf(w, "%-22s  %s\n", i18n.T(lang, key)+":", value)
		}
	}
	// The production addresses are the norm; only surface overridden endpoints.
	if ts.IAMBase != oauth.DefaultIAMBase {
		line("auth-field-iam", ts.IAMBase)
	}
	if ts.STSBase != oauth.DefaultSTSBase {
		line("auth-field-sts", ts.STSBase)
	}

	username := ts.Username
	var accountID, principalType string
	if claims, err := oauth.DecodeIDToken(ts.IDToken); err == nil {
		if username == "" {
			username = claims.Username
		}
		accountID = claims.AccountID
		principalType = claims.PrincipalType
	}
	line("auth-field-user", username)
	line("auth-field-account", accountID)
	line("auth-field-type", principalType)
	line("auth-field-scope", ts.Scope)
	line("auth-field-idexp", formatExpiry(ts.IDTokenExpiresAt))
	line("auth-field-rtexp", formatExpiry(ts.RefreshTokenExpiresAt))
	if ts.STS != nil {
		line("auth-field-stsexp", formatExpiry(ts.STS.Expiration))
	}
}

// authLoginCommand returns the `bce auth login` invocation that logs profile
// in (see oauth.LoginCommand).
func authLoginCommand(profile string) string {
	return oauth.LoginCommand(profile)
}

// printNotLoggedIn prints the text-mode "not logged in" notice, naming the
// profile and the login command that fixes it. The profile is named only when
// that command actually reaches it; see loginReachesProfile.
func printNotLoggedIn(cmd *cobra.Command, out io.Writer, profile string) {
	lang := i18n.GetLanguage()
	login := authLoginCommand(loginHintProfile(cmd, profile))
	var msg string
	if profile != "" && loginReachesProfile(cmd) {
		msg = fmt.Sprintf(i18n.T(lang, "auth-profile-not-logged-in"), profile, login)
	} else {
		msg = fmt.Sprintf(i18n.T(lang, "auth-not-logged-in"), login)
	}
	fmt.Fprintf(out, "%s %s\n", failMark(authStyled(cmd, out)), msg)
}

// loginReachesProfile reports whether the hinted login command logs in the
// profile the notice is about: always with an explicit --profile, otherwise
// only when the current profile is an OAuth profile. For an AK current profile
// a bare login goes to "oauth" instead, so the notice must not claim that the
// AK profile is the one that is not logged in.
func loginReachesProfile(cmd *cobra.Command) bool {
	if cmd.Root().PersistentFlags().Changed("profile") {
		return true
	}
	cfg, err := config.Load()
	if err != nil {
		return false
	}
	p := cfg.GetProfile("")
	return p != nil && p.Mode == config.ModeOAuth && oauth.ValidProfileName(p.Name)
}

// loginHintProfile returns the profile name to put in the login hint, or ""
// for a bare `bce auth login`. Without --profile the command acted on the
// current profile, and a bare login already reaches the right profile: the
// current one when it is an OAuth profile, "oauth" otherwise (so an AK profile
// is never converted). Only an explicit --profile is echoed back.
func loginHintProfile(cmd *cobra.Command, profile string) string {
	if cmd.Root().PersistentFlags().Changed("profile") {
		return profile
	}
	return ""
}

// resolveAuthProfile returns the profile a `bce auth` command operates on: the
// root --profile flag when given. Without it, status/refresh/logout
// (useCurrent) act on config.json's current profile. login acts on the current
// profile only when that is an OAuth profile (so re-running `bce auth login`
// reports or renews the login in use), and on defaultOAuthProfile otherwise,
// so a bare `bce auth login` never converts an AK profile. Names that would
// share a token file with another profile are rejected.
func resolveAuthProfile(cmd *cobra.Command, useCurrent bool) (string, error) {
	name, _ := cmd.Root().PersistentFlags().GetString("profile")
	fromCurrent := false
	if name == "" && useCurrent {
		cur, err := currentProfileName()
		if err != nil {
			return "", err
		}
		name, fromCurrent = cur, true
	}
	if name == "" && !useCurrent {
		if cfg, err := config.Load(); err == nil {
			if p := cfg.GetProfile(""); p != nil && p.Mode == config.ModeOAuth && oauth.ValidProfileName(p.Name) {
				return p.Name, nil
			}
		}
	}
	if name == "" {
		return defaultOAuthProfile, nil
	}
	if !oauth.ValidProfileName(name) {
		if fromCurrent {
			// No fallback to "oauth": logout/refresh would silently act on a
			// different login than the user's current profile.
			return "", fmt.Errorf("current profile %q cannot hold an OAuth login (use only letters, digits, '-' and '_'); pass --profile <name> to choose another", name)
		}
		return "", fmt.Errorf("invalid profile name %q for `bce auth`: use only letters, digits, '-' and '_'", name)
	}
	return name, nil
}

// currentProfileName reads config.json's current profile, so `bce auth
// status` can flag when it does not point at the oauth login it is
// reporting on — otherwise the reported credentials aren't actually the ones
// subsequent API calls would use.
func currentProfileName() (string, error) {
	cfg, err := config.Load()
	if err != nil {
		return "", err
	}
	return cfg.Current, nil
}

func formatExpiry(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Local().Format(time.RFC3339)
}

func init() {
	authLoginCmd.Flags().Bool("no-browser", false, i18n.FlagDesc(i18n.GetLanguage(), "auth-no-browser"))
	authLoginCmd.Flags().String("iam-base", "", i18n.FlagDesc(i18n.GetLanguage(), "auth-iam-base"))
	authLoginCmd.Flags().String("sts-base", "", i18n.FlagDesc(i18n.GetLanguage(), "auth-sts-base"))
	// Endpoint overrides exist for internal testing only; keep them out of --help.
	_ = authLoginCmd.Flags().MarkHidden("iam-base")
	_ = authLoginCmd.Flags().MarkHidden("sts-base")
	authLoginCmd.Flags().Int("callback-timeout", int(oauth.DefaultCallbackTimeout/time.Second), i18n.FlagDesc(i18n.GetLanguage(), "auth-callback-timeout"))
	for _, c := range []*cobra.Command{authLoginCmd, authStatusCmd, authLogoutCmd, authRefreshCmd} {
		c.Flags().String("format", "text", i18n.FlagDesc(i18n.GetLanguage(), "auth-format"))
	}

	authLogoutCmd.Flags().Bool("all", false, i18n.FlagDesc(i18n.GetLanguage(), "auth-all"))
	authLogoutCmd.Flags().BoolP("yes", "y", false, i18n.FlagDesc(i18n.GetLanguage(), "auth-yes"))

	authCmd.AddCommand(authLoginCmd, authStatusCmd, authLogoutCmd, authRefreshCmd)
	rootCmd.AddCommand(authCmd)
}
