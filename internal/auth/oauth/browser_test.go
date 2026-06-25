package oauth

import (
	"bytes"
	"errors"
	"runtime"
	"strings"
	"testing"
)

func withTestOpener(t *testing.T, fn func(string) error) {
	t.Helper()
	orig := browserOpener
	browserOpener = fn
	t.Cleanup(func() { browserOpener = orig })
}

func withNoTTY(t *testing.T, headless bool) {
	t.Helper()
	orig := noTTY
	noTTY = func() bool { return headless }
	t.Cleanup(func() { noTTY = orig })
}

func withPrintBuf(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	orig := printOut
	printOut = buf
	t.Cleanup(func() { printOut = orig })
	return buf
}

// neutralizeBrowserEnv clears the env vars OpenBrowser reads so a
// developer's outer $BROWSER / $HEYGEN_NO_BROWSER doesn't bleed into
// tests that assert opener invocation. Tests that *want* to exercise
// either branch should set them explicitly AFTER calling this.
func neutralizeBrowserEnv(t *testing.T) {
	t.Helper()
	t.Setenv("BROWSER", "")
	t.Setenv("HEYGEN_NO_BROWSER", "")
}

func TestOpenBrowser_TTYInvokesOpener(t *testing.T) {
	neutralizeBrowserEnv(t)
	withNoTTY(t, false)
	called := ""
	withTestOpener(t, func(u string) error {
		called = u
		return nil
	})
	buf := withPrintBuf(t)

	if err := OpenBrowser("https://example.test/oauth/authorize?x=1"); err != nil {
		t.Fatalf("OpenBrowser: %v", err)
	}
	if called != "https://example.test/oauth/authorize?x=1" {
		t.Errorf("opener called with %q", called)
	}
	if buf.Len() != 0 {
		t.Errorf("expected no print on TTY success, got %q", buf.String())
	}
}

func TestOpenBrowser_NonTTYPrintsAndSkips(t *testing.T) {
	neutralizeBrowserEnv(t)
	withNoTTY(t, true)
	called := false
	withTestOpener(t, func(u string) error {
		called = true
		return nil
	})
	buf := withPrintBuf(t)

	if err := OpenBrowser("https://example.test/x"); err != nil {
		t.Fatalf("OpenBrowser: %v", err)
	}
	if called {
		t.Error("opener should not be called when stdin is non-TTY")
	}
	if !strings.Contains(buf.String(), "https://example.test/x") {
		t.Errorf("expected URL in print output, got %q", buf.String())
	}
}

func TestOpenBrowser_OpenerFailureFallsBack(t *testing.T) {
	neutralizeBrowserEnv(t)
	withNoTTY(t, false)
	withTestOpener(t, func(u string) error {
		return errors.New("no display")
	})
	buf := withPrintBuf(t)

	err := OpenBrowser("https://example.test/x")
	if err == nil {
		t.Fatal("expected opener error to surface")
	}
	if !strings.Contains(buf.String(), "https://example.test/x") {
		t.Errorf("expected fallback print, got %q", buf.String())
	}
	if !strings.Contains(buf.String(), "Could not open browser") {
		t.Errorf("expected reason in print, got %q", buf.String())
	}
}

func TestOpenBrowser_HEYGEN_NO_BROWSEREnvSkips(t *testing.T) {
	neutralizeBrowserEnv(t)
	withNoTTY(t, false)
	t.Setenv("HEYGEN_NO_BROWSER", "1")
	called := false
	withTestOpener(t, func(u string) error {
		called = true
		return nil
	})
	buf := withPrintBuf(t)

	if err := OpenBrowser("https://example.test/x"); err != nil {
		t.Fatalf("OpenBrowser: %v", err)
	}
	if called {
		t.Error("opener should not run when HEYGEN_NO_BROWSER=1")
	}
	if !strings.Contains(buf.String(), "https://example.test/x") {
		t.Errorf("expected URL in print, got %q", buf.String())
	}
}

func TestOpenBrowser_BROWSERNoneEnvSkips(t *testing.T) {
	neutralizeBrowserEnv(t)
	withNoTTY(t, false)
	t.Setenv("BROWSER", "none")
	called := false
	withTestOpener(t, func(u string) error {
		called = true
		return nil
	})
	buf := withPrintBuf(t)

	if err := OpenBrowser("https://example.test/x"); err != nil {
		t.Fatalf("OpenBrowser: %v", err)
	}
	if called {
		t.Error("opener should not run when BROWSER=none")
	}
	if !strings.Contains(buf.String(), "https://example.test/x") {
		t.Errorf("expected URL in print, got %q", buf.String())
	}
}

func TestOpenBrowser_RejectsEmptyURL(t *testing.T) {
	if err := OpenBrowser(""); err == nil {
		t.Fatal("expected error for empty URL")
	}
}

// platformOpenCmd's $BROWSER precedence is a Linux/BSD convention — on
// darwin/windows we always dispatch to the native opener regardless of
// $BROWSER, so these tests are skipped there.
func TestPlatformOpenCmd_HonorsBROWSEROnLinux(t *testing.T) {
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		t.Skip("$BROWSER precedence is Linux/BSD only")
	}
	t.Setenv("BROWSER", "firefox")
	cmd := platformOpenCmd("https://example.test/x")
	if len(cmd.Args) < 2 || cmd.Args[0] != "firefox" || cmd.Args[1] != "https://example.test/x" {
		t.Errorf("expected [firefox <url>], got %v", cmd.Args)
	}
}

func TestPlatformOpenCmd_BROWSERNoneFallsThroughOnLinux(t *testing.T) {
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		t.Skip("$BROWSER precedence is Linux/BSD only")
	}
	// "none" is handled by OpenBrowser itself, never reaching
	// platformOpenCmd — but defend the invariant: if it ever did,
	// don't try to exec "none".
	t.Setenv("BROWSER", "none")
	cmd := platformOpenCmd("https://example.test/x")
	if len(cmd.Args) < 1 || cmd.Args[0] != "xdg-open" {
		t.Errorf("expected xdg-open fallback when BROWSER=none, got %v", cmd.Args)
	}
}

func TestPlatformOpenCmd_EmptyBROWSERFallsThroughOnLinux(t *testing.T) {
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		t.Skip("$BROWSER precedence is Linux/BSD only")
	}
	t.Setenv("BROWSER", "")
	cmd := platformOpenCmd("https://example.test/x")
	if len(cmd.Args) < 1 || cmd.Args[0] != "xdg-open" {
		t.Errorf("expected xdg-open fallback when BROWSER empty, got %v", cmd.Args)
	}
}
