package cmd

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/baidubce/bce-cli/internal/i18n"
	"github.com/baidubce/bce-cli/internal/oauth"
	"github.com/spf13/cobra"
)

// printAlreadyAuthenticated explains that a login already exists and how to
// replace it. Only the account identifier and the token lifetime are shown; no
// token material is ever printed. logoutCmd is the exact command that would end
// the reported session.
func printAlreadyAuthenticated(cmd *cobra.Command, ts *oauth.TokenSet, logoutCmd string) {
	lang := i18n.GetLanguage()
	w := cmd.OutOrStdout()
	styled := authStyled(cmd, w)

	headline := i18n.T(lang, "auth-already-anonymous")
	if id := identityOf(ts).Identity(); id != "" {
		headline = fmt.Sprintf(i18n.T(lang, "auth-already"), bold(id, styled))
	}

	// A cross (not a check mark) is used because login is refusing to
	// proceed, not confirming success — the account below is simply already
	// signed in.
	fmt.Fprintf(w, "%s %s\n", failMark(styled), headline)
	if d := sessionRemaining(ts); d > 0 {
		fmt.Fprintf(w, i18n.T(lang, "auth-already-expires")+"\n", humanizeDuration(d))
	}
	fmt.Fprintf(w, i18n.T(lang, "auth-already-relogin")+"\n", bold(logoutCmd, styled))
}

// sessionDeadline returns the moment this login actually stops working, or the
// zero time when it does not expire. It mirrors oauth.loginActive's rules
// exactly, so a session oauth.ActiveLogin reports as active never renders here
// as already expired:
//   - a Refresh Token with no expiry claim (IsZero) never expires;
//   - otherwise the latest still-future expiry among the Refresh Token and the
//     ID Token applies, since a session survives as long as either can renew
//     credentials.
func sessionDeadline(ts *oauth.TokenSet) time.Time {
	if ts.RefreshToken != "" && ts.RefreshTokenExpiresAt.IsZero() {
		return time.Time{}
	}
	now := time.Now()
	var deadline time.Time
	if ts.RefreshToken != "" && ts.RefreshTokenExpiresAt.After(now) {
		deadline = ts.RefreshTokenExpiresAt
	}
	if ts.IDTokenExpiresAt.After(now) && ts.IDTokenExpiresAt.After(deadline) {
		deadline = ts.IDTokenExpiresAt
	}
	return deadline
}

// sessionRemaining reports how long the login stays usable without a new
// browser sign-in. A zero duration means either "already expired" or "does not
// expire" — callers that need to tell those apart should check sessionDeadline
// directly.
func sessionRemaining(ts *oauth.TokenSet) time.Duration {
	deadline := sessionDeadline(ts)
	if deadline.IsZero() {
		return 0
	}
	return time.Until(deadline)
}

// humanizeDuration renders a duration as "47h 53m", "12m" or "<1m".
func humanizeDuration(d time.Duration) string {
	if d < time.Minute {
		return "<1m"
	}
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}

// bold wraps text in the ANSI bold sequence when styling is enabled.
func bold(s string, enabled bool) string {
	if !enabled || s == "" {
		return s
	}
	return "\033[1m" + s + "\033[0m"
}

// red colours text, matching the palette used for JSON output.
func red(s string, enabled bool) string {
	if !enabled || s == "" {
		return s
	}
	return "\033[0;31m" + s + "\033[0m"
}

// green colours text, matching the palette used for JSON output.
func green(s string, enabled bool) string {
	if !enabled || s == "" {
		return s
	}
	return "\033[0;32m" + s + "\033[0m"
}

// yellow colours text used for non-fatal notices (e.g. current profile
// pointing elsewhere), distinct from the pass/fail green/red palette.
func yellow(s string, enabled bool) string {
	if !enabled || s == "" {
		return s
	}
	return "\033[0;33m" + s + "\033[0m"
}

// authStyled reports whether ANSI styling should be applied for this command's
// output: --no-color was not requested, and the destination is a terminal.
func authStyled(cmd *cobra.Command, w io.Writer) bool {
	noColor, _ := cmd.Root().PersistentFlags().GetBool("no-color")
	return !noColor && writerIsTerminal(w)
}

// okMark and failMark prefix a line reporting a successful or failed
// text-format auth operation. warnMark prefixes a non-fatal notice.
func okMark(styled bool) string   { return green("✓", styled) }
func failMark(styled bool) string { return red("✗", styled) }
func warnMark(styled bool) string { return yellow("⚠", styled) }

// writerIsTerminal reports whether the destination we are about to write to is
// a terminal. Basing this on the actual writer rather than os.Stdout keeps
// styling out of redirected output.
func writerIsTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}
