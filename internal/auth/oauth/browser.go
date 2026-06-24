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
// Env-var precedence on Linux/BSD:
//
//  1. HEYGEN_NO_BROWSER=1 — never try to open a browser; print the URL.
//  2. BROWSER=none — same, mirrors the cross-distro convention.
//  3. BROWSER=<command> (any other non-empty value) — invoke <command>
//     with the URL as a single argument, per the de-facto Linux
//     convention sketched in freedesktop.org and adopted by Python's
//     webbrowser module, Git, etc.
//  4. Fallback: xdg-open <url>.
//
// On macOS and Windows the platform-native opener is used regardless of
// $BROWSER, since the conventional values there ("Safari", "Chrome",
// "msedge") aren't shell commands.
func OpenBrowser(url string) error {
	if url == "" {
		return errors.New("oauth: OpenBrowser called with empty URL")
	}
	if os.Getenv("HEYGEN_NO_BROWSER") == "1" || os.Getenv("BROWSER") == "none" {
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
	cmd := platformOpenCmd(url)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("oauth: launch browser via %s: %w", cmd.Path, err)
	}
	// Don't wait on the child — `open` / `xdg-open` / $BROWSER may stay
	// attached to the launched browser for the user's whole session.
	go func() { _ = cmd.Wait() }()
	return nil
}

// platformOpenCmd builds the platform-native exec.Cmd that opens url.
// Factored out so the env-var precedence is testable without spawning
// real processes.
func platformOpenCmd(url string) *exec.Cmd {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url)
	case "windows":
		// rundll32 is the most reliable way to launch a URL on Windows
		// without needing a specific app association.
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		// Linux + BSD: honor the user's $BROWSER preference (non-empty,
		// not the "none" sentinel handled in OpenBrowser) before
		// falling back to xdg-open. $BROWSER is the user's own shell
		// env — the documented Linux convention is to invoke its value
		// as a command, so the "taint" is exactly the contract.
		if b := os.Getenv("BROWSER"); b != "" && b != "none" {
			return exec.Command(b, url) //nolint:gosec // $BROWSER is the user's documented preference; invoking it as a command is the convention.
		}
		return exec.Command("xdg-open", url)
	}
}

func printManualURL(url, reason string) {
	if reason != "" {
		fmt.Fprintf(printOut, "Could not open browser automatically (%s).\n", reason)
	}
	fmt.Fprintf(printOut, "Open this URL manually to continue:\n  %s\n", url)
}
