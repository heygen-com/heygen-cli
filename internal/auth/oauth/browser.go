package oauth

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"

	"golang.org/x/term"
)

// browserOpener is overridable in tests so we don't actually spawn `open`
// / `xdg-open` / `rundll32` during unit runs.
var browserOpener = openWithSystem

// noTTY is overridable in tests so we can simulate a non-TTY stdin
// without re-plumbing os.Stdin.
var noTTY = func() bool {
	return !term.IsTerminal(int(os.Stdin.Fd()))
}

// printOut is overridable in tests so we can capture the headless-
// fallback URL print without competing for os.Stdout.
var printOut io.Writer = os.Stdout

// OpenBrowser launches the user's default browser at the given URL.
//
// When stdin is not a TTY (headless / CI / SSH session), or when the
// platform "open" command fails, the URL is printed to stdout instead
// so the user can copy/paste it manually. The error return covers
// genuine launch failures the caller may want to log; callers should
// still proceed to wait on the loopback callback either way.
//
// Two env-var escape hatches mirror the hyperframes-CLI driver:
//
//   - BROWSER=none     — never try to open a browser; print the URL.
//   - HEYGEN_NO_BROWSER=1 — same, scoped to this CLI.
func OpenBrowser(url string) error {
	if url == "" {
		return errors.New("oauth: OpenBrowser called with empty URL")
	}
	if os.Getenv("BROWSER") == "none" || os.Getenv("HEYGEN_NO_BROWSER") == "1" {
		printManualURL(url, "")
		return nil
	}
	if noTTY() {
		printManualURL(url, "")
		return nil
	}
	if err := browserOpener(url); err != nil {
		printManualURL(url, err.Error())
		return err
	}
	return nil
}

// openWithSystem dispatches to the platform-native command.
func openWithSystem(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		// rundll32 is the most reliable way to launch a URL on Windows
		// without needing a specific app association.
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		// Linux + BSD: xdg-open is the conventional dispatcher.
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("oauth: launch browser via %s: %w", cmd.Path, err)
	}
	// Don't wait on the child — `open` / `xdg-open` may stay attached to
	// the launched browser for the user's whole session.
	go func() { _ = cmd.Wait() }()
	return nil
}

func printManualURL(url, reason string) {
	if reason != "" {
		fmt.Fprintf(printOut, "Could not open browser automatically (%s).\n", reason)
	}
	fmt.Fprintf(printOut, "Open this URL manually to continue:\n  %s\n", url)
}
