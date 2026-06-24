package oauth

import (
	"bytes"
	"errors"
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

func TestOpenBrowser_TTYInvokesOpener(t *testing.T) {
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
