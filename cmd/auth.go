package cmd

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/baidubce/bce-cli/internal/config"
	"github.com/baidubce/bce-cli/internal/i18n"
	"github.com/baidubce/bce-cli/internal/oauth"
	"github.com/spf13/cobra"
)

// defaultOAuthProfile is the single profile `bce auth` operates on. OAuth login
// is a single-account flow (see oauth.ActiveLogin), so there is no
// --profile flag to route login/status/logout to a different one — a fixed
// name keeps the config.json entry and the on-disk token file unambiguous.
const defaultOAuthProfile = "oauth"

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
		if err := rejectProfileFlag(cmd); err != nil {
			return err
		}
		format, err := authFormat(cmd)
		if err != nil {
			return err
		}
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		name := defaultOAuthProfile
		lang := i18n.GetLanguage()

		// Only one account may be logged in at a time: report the existing
		// session instead of starting a second browser flow. ActiveLogin only
		// looks at this one fixed profile, so a leftover token file under a
		// different name can never block a fresh login here. This guard only
		// applies while "oauth" is also the current profile — if the user has
		// switched current elsewhere (e.g. back to an AK profile), a fresh
		// login proceeds and simply replaces the old oauth.json on success;
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
		if err := rejectProfileFlag(cmd); err != nil {
			return err
		}
		format, err := authFormat(cmd)
		if err != nil {
			return err
		}
		ts, err := oauth.NewManager(defaultOAuthProfile).Status()
		if err != nil {
			if err == oauth.ErrNotLoggedIn {
				if format == "json" {
					return writeAuthJSON(cmd, []jsonField{
						{"authenticated", false},
						{"message", "Not logged in"},
					})
				}
				out := cmd.OutOrStdout()
				fmt.Fprintf(out, "%s %s\n", failMark(authStyled(cmd, out)), i18n.T(i18n.GetLanguage(), "auth-not-logged-in"))
				return nil
			}
			return err
		}
		if format == "json" {
			return writeLoggedInJSON(cmd, ts, nil)
		}
		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "%s %s\n", okMark(authStyled(cmd, out)), i18n.T(i18n.GetLanguage(), "auth-authenticated"))
		printNotCurrentNotice(cmd, out)
		fmt.Fprintf(out, "%-22s  %s\n", i18n.T(i18n.GetLanguage(), "auth-field-profile")+":", defaultOAuthProfile)
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
		if err := rejectProfileFlag(cmd); err != nil {
			return err
		}
		format, err := authFormat(cmd)
		if err != nil {
			return err
		}
		mgr := oauth.NewManager(defaultOAuthProfile)

		if _, err := mgr.Status(); err == oauth.ErrNotLoggedIn {
			if format == "json" {
				return writeAuthJSON(cmd, []jsonField{
					{"success", false},
					{"authenticated", false},
					{"message", "Not logged in"},
				})
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "%s %s\n", failMark(authStyled(cmd, out)), i18n.T(i18n.GetLanguage(), "auth-not-logged-in"))
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
			return writeLoggedInJSON(cmd, ts, []jsonField{{"success", true}})
		}
		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "%s %s\n", okMark(authStyled(cmd, out)), i18n.T(i18n.GetLanguage(), "auth-refreshed"))
		printNotCurrentNotice(cmd, out)
		fmt.Fprintf(out, "%-22s  %s\n", i18n.T(i18n.GetLanguage(), "auth-field-profile")+":", defaultOAuthProfile)
		printAuthStatus(cmd, ts)
		return nil
	},
}

// writeLoggedInJSON renders the shared logged-in payload (authenticated + the
// session detail fields, plus current_profile when it differs) used by both
// `status` and `refresh`. prefix carries any command-specific leading fields
// (e.g. success for refresh) placed before "authenticated".
func writeLoggedInJSON(cmd *cobra.Command, ts *oauth.TokenSet, prefix []jsonField) error {
	fields := append(append([]jsonField{}, prefix...), jsonField{"authenticated", true})
	fields = append(fields, authStatusJSONFields(defaultOAuthProfile, ts)...)
	if cur, err := currentProfileName(); err == nil && cur != defaultOAuthProfile {
		fields = append(fields, jsonField{"current_profile", cur})
	}
	return writeAuthJSON(cmd, fields)
}

// printNotCurrentNotice warns, in text mode, when the current profile is not
// the oauth login being reported — the reported credentials are not the ones
// subsequent API calls would actually use until the user switches.
func printNotCurrentNotice(cmd *cobra.Command, out io.Writer) {
	if cur, err := currentProfileName(); err == nil && cur != defaultOAuthProfile {
		fmt.Fprintf(out, "%s %s\n", warnMark(authStyled(cmd, out)), fmt.Sprintf(i18n.T(i18n.GetLanguage(), "auth-not-current"), cur, defaultOAuthProfile))
	}
}

var authLogoutCmd = &cobra.Command{
	Use:         "logout",
	Short:       i18n.T(i18n.GetLanguage(), "cmd-auth-logout"),
	Annotations: map[string]string{"i18n-short": "cmd-auth-logout"},
	Args:        cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := rejectProfileFlag(cmd); err != nil {
			return err
		}
		format, err := authFormat(cmd)
		if err != nil {
			return err
		}
		mgr := oauth.NewManager(defaultOAuthProfile)

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
			fmt.Fprintf(out, "%s %s\n", failMark(authStyled(cmd, out)), i18n.T(i18n.GetLanguage(), "auth-not-logged-in"))
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

// rejectProfileFlag errors out if the caller passed --profile explicitly.
// bce auth is a single-account flow (see defaultOAuthProfile): the flag is
// still visible in --help because it is a root-level persistent flag shared
// with every other command, but silently accepting it here would let a user
// believe `--profile foo` selects which login they're touching when it never
// did anything at all.
func rejectProfileFlag(cmd *cobra.Command) error {
	if cmd.Root().PersistentFlags().Changed("profile") {
		return fmt.Errorf("--profile is not supported by `bce auth`: it always operates on the single %q login; run `bce auth logout` first to replace it", defaultOAuthProfile)
	}
	return nil
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

	authCmd.AddCommand(authLoginCmd, authStatusCmd, authLogoutCmd, authRefreshCmd)
	rootCmd.AddCommand(authCmd)
}
