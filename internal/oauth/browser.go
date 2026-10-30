package oauth

import (
	"fmt"
	"os/exec"
	"runtime"
)

// OpenBrowser launches the system browser for the given URL.
// Callers must still print the URL so the user can open it manually when this
// fails (headless shells, SSH sessions, restricted environments).
func OpenBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("open browser: %w", err)
	}
	// The browser outlives this process; reap the launcher in the background so
	// it does not linger as a zombie.
	go func() { _ = cmd.Wait() }()
	return nil
}
