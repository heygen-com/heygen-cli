package main

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// dispatchSpy records which runner runAuthLogin ended up invoking so
// the table-driven dispatch tests can assert against a stable value
// without spinning up the real OAuth / API-key flows.
type dispatchSpy struct {
	oauthCalls   int
	apiKeyCalls  int
	pickerCalls  int
	pickerChoice loginChoice
	pickerErr    error
}

func (s *dispatchSpy) deps() *runAuthLoginDeps {
	return &runAuthLoginDeps{
		stdinIsTerminal:  func() bool { return true },
		stdoutIsTerminal: func() bool { return true },
		nonInteractive:   func() bool { return false },
		runPicker: func(ctx context.Context, stdin io.Reader, stderr io.Writer) (loginChoice, error) {
			s.pickerCalls++
			return s.pickerChoice, s.pickerErr
		},
		runOAuth: func(c *cobra.Command, x *cmdContext) error {
			s.oauthCalls++
			return nil
		},
		runAPIKey: func(c *cobra.Command, x *cmdContext) error {
			s.apiKeyCalls++
			return nil
		},
	}
}

// makeDispatchCmd builds a minimal cobra command + context that
// runAuthLogin will accept. Tests then call runAuthLogin directly with
// the spy-backed deps; we never actually Execute() because we don't
// need cobra's argv parsing for dispatch testing.
func makeDispatchCmd(t *testing.T) (*cobra.Command, *cmdContext) {
	t.Helper()
	cmd := &cobra.Command{Use: "login"}
	cmd.SetIn(strings.NewReader(""))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetContext(context.Background())
	return cmd, &cmdContext{}
}

func TestRunAuthLogin_Dispatch(t *testing.T) {
	cases := []struct {
		name            string
		flags           authLoginFlags
		stdinTTY        bool
		stdoutTTY       bool
		nonInteractive  bool
		pickerChoice    loginChoice
		wantOAuthCalls  int
		wantAPIKeyCalls int
		wantPickerCalls int
		wantErr         bool
	}{
		{
			name:           "flag --oauth always routes to OAuth runner",
			flags:          authLoginFlags{oauthMode: true},
			stdinTTY:       false, // even on non-TTY
			stdoutTTY:      false,
			nonInteractive: true,
			wantOAuthCalls: 1,
		},
		{
			name:            "flag --api-key always routes to API-key runner",
			flags:           authLoginFlags{apiKeyMode: true},
			stdinTTY:        true, // even on TTY
			stdoutTTY:       true,
			wantAPIKeyCalls: 1,
		},
		{
			name:            "no flag + TTY + interactive + picker picks OAuth",
			stdinTTY:        true,
			stdoutTTY:       true,
			pickerChoice:    loginChoiceOAuth,
			wantPickerCalls: 1,
			wantOAuthCalls:  1,
		},
		{
			name:            "no flag + TTY + interactive + picker picks API key",
			stdinTTY:        true,
			stdoutTTY:       true,
			pickerChoice:    loginChoiceAPIKey,
			wantPickerCalls: 1,
			wantAPIKeyCalls: 1,
		},
		{
			name:            "no flag + stdin not TTY → API key, no picker",
			stdinTTY:        false,
			stdoutTTY:       true,
			wantAPIKeyCalls: 1,
		},
		{
			name:            "no flag + stdout not TTY → API key, no picker",
			stdinTTY:        true,
			stdoutTTY:       false,
			wantAPIKeyCalls: 1,
		},
		{
			name:            "no flag + CI=true → API key even on TTY",
			stdinTTY:        true,
			stdoutTTY:       true,
			nonInteractive:  true,
			wantAPIKeyCalls: 1,
		},
		{
			name:    "device-code flag is rejected with usage error",
			flags:   authLoginFlags{deviceCodeMode: true},
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spy := &dispatchSpy{pickerChoice: tc.pickerChoice}
			deps := spy.deps()
			deps.stdinIsTerminal = func() bool { return tc.stdinTTY }
			deps.stdoutIsTerminal = func() bool { return tc.stdoutTTY }
			deps.nonInteractive = func() bool { return tc.nonInteractive }
			runAuthLoginTestDeps = deps
			t.Cleanup(func() { runAuthLoginTestDeps = nil })

			cmd, ctx := makeDispatchCmd(t)
			err := runAuthLogin(cmd, ctx, tc.flags)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want err, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("runAuthLogin: %v", err)
			}
			if spy.oauthCalls != tc.wantOAuthCalls {
				t.Errorf("oauthCalls = %d, want %d", spy.oauthCalls, tc.wantOAuthCalls)
			}
			if spy.apiKeyCalls != tc.wantAPIKeyCalls {
				t.Errorf("apiKeyCalls = %d, want %d", spy.apiKeyCalls, tc.wantAPIKeyCalls)
			}
			if spy.pickerCalls != tc.wantPickerCalls {
				t.Errorf("pickerCalls = %d, want %d", spy.pickerCalls, tc.wantPickerCalls)
			}
		})
	}
}

// TestRunAuthLogin_PickerCancelMapsToCanceled exercises the explicit
// branch that converts the picker's sentinel error into the
// "canceled" CLI error the rest of the CLI uses for user-initiated
// aborts. Without this round-trip the user would see a generic
// "login picker failed" message on Esc / Ctrl-C.
func TestRunAuthLogin_PickerCancelMapsToCanceled(t *testing.T) {
	spy := &dispatchSpy{pickerErr: pickerCanceledError{}}
	deps := spy.deps()
	runAuthLoginTestDeps = deps
	t.Cleanup(func() { runAuthLoginTestDeps = nil })

	cmd, ctx := makeDispatchCmd(t)
	err := runAuthLogin(cmd, ctx, authLoginFlags{})
	if err == nil {
		t.Fatalf("want canceled error, got nil")
	}
	if !strings.Contains(err.Error(), "canceled") {
		t.Errorf("err = %q, want 'canceled' substring", err.Error())
	}
	if spy.oauthCalls != 0 || spy.apiKeyCalls != 0 {
		t.Errorf("no runner should have been invoked; oauth=%d apikey=%d", spy.oauthCalls, spy.apiKeyCalls)
	}
}

// TestRunAuthLogin_PickerNonCancelErrorPropagates makes sure that any
// non-cancel error from the picker bubbles up unchanged rather than
// being swallowed by the cancel-conversion branch.
func TestRunAuthLogin_PickerNonCancelErrorPropagates(t *testing.T) {
	sentinel := errors.New("tea: boom")
	spy := &dispatchSpy{pickerErr: sentinel}
	deps := spy.deps()
	runAuthLoginTestDeps = deps
	t.Cleanup(func() { runAuthLoginTestDeps = nil })

	cmd, ctx := makeDispatchCmd(t)
	err := runAuthLogin(cmd, ctx, authLoginFlags{})
	if err == nil {
		t.Fatalf("want error, got nil")
	}
	if !strings.Contains(err.Error(), "tea: boom") {
		t.Errorf("err = %q, want 'tea: boom'", err.Error())
	}
}

// TestAuthLogin_NoFlag_NonTTY_DefaultsToAPIKey is the end-to-end
// counterpart to the table tests above. It drives the whole cobra
// command tree (newRootCmd + Execute) with no auth flag and a piped
// stdin, and verifies that the API-key flow ran (credentials file
// written) while the OAuth flow did not (no loopback dance attempted,
// no browser-open noise). This guards the team's "non-interactive
// defaults to API key" decision against future refactors that bypass
// the dispatcher.
func TestAuthLogin_NoFlag_NonTTY_DefaultsToAPIKey(t *testing.T) {
	// go test never has a TTY on stdin/stdout, so the dispatcher's
	// real (not stubbed) TTY detection naturally returns false here.
	// We still clear the env vars explicitly to avoid CI surprises.
	t.Setenv("HEYGEN_NONINTERACTIVE", "")
	t.Setenv("CI", "")
	dir := t.TempDir()
	t.Setenv("HEYGEN_CONFIG_DIR", dir)

	res := runCommandWithInput(t, "http://example.invalid", "", strings.NewReader("piped-key\n"),
		"auth", "login")
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s\nstdout: %s", res.ExitCode, res.Stderr, res.Stdout)
	}
	if got := storedAPIKey(t, dir); got != "piped-key" {
		t.Fatalf("stored api_key = %q, want %q", got, "piped-key")
	}
}

// TestNonInteractiveEnvFunc_ParsesCommonShapes documents the rules the
// env override uses: any non-empty value other than "0"/"false" counts
// as enabling the non-interactive path. The vars share the
// same semantics, so we test them together.
func TestNonInteractiveEnvFunc_ParsesCommonShapes(t *testing.T) {
	cases := []struct {
		envKey   string
		envValue string
		want     bool
	}{
		{"HEYGEN_NONINTERACTIVE", "1", true},
		{"HEYGEN_NONINTERACTIVE", "true", true},
		{"HEYGEN_NONINTERACTIVE", "yes", true},
		{"HEYGEN_NONINTERACTIVE", "0", false},
		{"HEYGEN_NONINTERACTIVE", "false", false},
		{"HEYGEN_NONINTERACTIVE", "", false},
		{"CI", "true", true},
		{"CI", "1", true},
		{"CI", "false", false},
		{"CI", "0", false},
		{"CI", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.envKey+"="+tc.envValue, func(t *testing.T) {
			t.Setenv("HEYGEN_NONINTERACTIVE", "")
			t.Setenv("CI", "")
			t.Setenv(tc.envKey, tc.envValue)
			got := nonInteractiveEnvFunc()
			if got != tc.want {
				t.Errorf("nonInteractiveEnvFunc() = %v, want %v (env %s=%q)", got, tc.want, tc.envKey, tc.envValue)
			}
		})
	}
}
