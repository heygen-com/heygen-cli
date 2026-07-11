package main

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// loginAnalyticsSpy is a test double for loginAnalyticsClient (U3/U4): it
// records every IdentifyAccount / AuthLoginStarted / AuthLoginCompleted /
// AuthLoginFailed call in order so tests can assert both the exact call
// and how it reconciles against the others.
type loginAnalyticsSpy struct {
	identifyCalls []string
	started       []string
	completed     []string
	failed        []loginAnalyticsFailedCall
}

type loginAnalyticsFailedCall struct {
	method string
	reason string
}

func (s *loginAnalyticsSpy) IdentifyAccount(distinctId string) {
	s.identifyCalls = append(s.identifyCalls, distinctId)
}

func (s *loginAnalyticsSpy) AuthLoginStarted(method string) {
	s.started = append(s.started, method)
}

func (s *loginAnalyticsSpy) AuthLoginCompleted(method string) {
	s.completed = append(s.completed, method)
}

func (s *loginAnalyticsSpy) AuthLoginFailed(method, reason string) {
	s.failed = append(s.failed, loginAnalyticsFailedCall{method: method, reason: reason})
}

// withLoginAnalyticsSpy swaps the package-level loginAnalytics for a spy
// for the duration of the test, restoring the previous value on cleanup.
// Every entry point that fires login telemetry (the dispatcher, the two
// runners, or a full cmd.Execute()) reads the same package var, so this
// works regardless of how the test drives the login flow.
func withLoginAnalyticsSpy(t *testing.T) *loginAnalyticsSpy {
	t.Helper()
	spy := &loginAnalyticsSpy{}
	orig := loginAnalytics
	loginAnalytics = spy
	t.Cleanup(func() { loginAnalytics = orig })
	return spy
}

// erroringReader always fails on Read, simulating an aborted stdin read
// (e.g. Ctrl-C mid-input) for the api-key login flow.
type erroringReader struct{}

func (erroringReader) Read(_ []byte) (int, error) {
	return 0, errors.New("input aborted")
}

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

// U4: AuthLoginStarted fires once per dispatched attempt, after the
// picker/flag resolves a method — using the same stubbed runOAuth/runAPIKey
// as TestRunAuthLogin_Dispatch (so only Started is observable here; the
// real runners' Completed/Failed calls are exercised separately in
// auth_login_oauth_test.go where the real runOAuthLogin/runAPIKeyLogin run).
func TestRunAuthLogin_TelemetryStarted(t *testing.T) {
	cases := []struct {
		name         string
		flags        authLoginFlags
		pickerChoice loginChoice
		wantStarted  []string
	}{
		{
			name:        "--oauth flag",
			flags:       authLoginFlags{oauthMode: true},
			wantStarted: []string{"oauth"},
		},
		{
			name:        "--api-key flag",
			flags:       authLoginFlags{apiKeyMode: true},
			wantStarted: []string{"api_key"},
		},
		{
			name:         "picker picks oauth",
			pickerChoice: loginChoiceOAuth,
			wantStarted:  []string{"oauth"},
		},
		{
			name:         "picker picks api key",
			pickerChoice: loginChoiceAPIKey,
			wantStarted:  []string{"api_key"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spy := withLoginAnalyticsSpy(t)
			dispatchTestSpy := &dispatchSpy{pickerChoice: tc.pickerChoice}
			runAuthLoginTestDeps = dispatchTestSpy.deps()
			t.Cleanup(func() { runAuthLoginTestDeps = nil })

			cmd, ctx := makeDispatchCmd(t)
			if err := runAuthLogin(cmd, ctx, tc.flags); err != nil {
				t.Fatalf("runAuthLogin: %v", err)
			}

			if !slices.Equal(spy.started, tc.wantStarted) {
				t.Errorf("started = %v, want %v", spy.started, tc.wantStarted)
			}
			// The stubbed runners never call Completed/Failed themselves —
			// only the dispatcher's Started call is under test here.
			if len(spy.completed) != 0 {
				t.Errorf("completed = %v, want none (runner is stubbed)", spy.completed)
			}
			if len(spy.failed) != 0 {
				t.Errorf("failed = %v, want none (runner is stubbed)", spy.failed)
			}
		})
	}
}

// U4: a picker cancellation must emit no started/failed pair at all —
// Started only fires after a method is chosen.
func TestRunAuthLogin_PickerCancel_NoTelemetry(t *testing.T) {
	spy := withLoginAnalyticsSpy(t)
	dispatchTestSpy := &dispatchSpy{pickerErr: pickerCanceledError{}}
	runAuthLoginTestDeps = dispatchTestSpy.deps()
	t.Cleanup(func() { runAuthLoginTestDeps = nil })

	cmd, ctx := makeDispatchCmd(t)
	if err := runAuthLogin(cmd, ctx, authLoginFlags{}); err == nil {
		t.Fatal("want canceled error, got nil")
	}

	if len(spy.started) != 0 || len(spy.completed) != 0 || len(spy.failed) != 0 {
		t.Fatalf("expected no telemetry on picker cancel; started=%v completed=%v failed=%v",
			spy.started, spy.completed, spy.failed)
	}
}

// U4: --device-code emits started(device_code) then
// failed(device_code, device_code_unsupported) — the reason is explicitly
// named in the plan's enum, so this implementation instruments it rather
// than silently dropping telemetry for the flag.
func TestRunAuthLogin_DeviceCode_Telemetry(t *testing.T) {
	spy := withLoginAnalyticsSpy(t)

	cmd, ctx := makeDispatchCmd(t)
	if err := runAuthLogin(cmd, ctx, authLoginFlags{deviceCodeMode: true}); err == nil {
		t.Fatal("want usage error, got nil")
	}

	if got := spy.started; len(got) != 1 || got[0] != "device_code" {
		t.Fatalf("started = %v, want [device_code]", got)
	}
	if got := spy.failed; len(got) != 1 || got[0] != (loginAnalyticsFailedCall{"device_code", "device_code_unsupported"}) {
		t.Fatalf("failed = %v, want [{device_code device_code_unsupported}]", got)
	}
}

// U4: --api-key with an aborted stdin read (Ctrl-C mid-input) emits
// started(api_key) then failed(api_key, api_key_aborted).
func TestRunAuthLogin_APIKeyAborted_Telemetry(t *testing.T) {
	spy := withLoginAnalyticsSpy(t)
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())

	cmd, ctx := makeDispatchCmd(t)
	cmd.SetIn(erroringReader{})

	if err := runAuthLogin(cmd, ctx, authLoginFlags{apiKeyMode: true}); err == nil {
		t.Fatal("want error, got nil")
	}

	if got := spy.started; len(got) != 1 || got[0] != "api_key" {
		t.Fatalf("started = %v, want [api_key]", got)
	}
	if got := spy.failed; len(got) != 1 || got[0] != (loginAnalyticsFailedCall{"api_key", "api_key_aborted"}) {
		t.Fatalf("failed = %v, want [{api_key api_key_aborted}]", got)
	}
	if len(spy.completed) != 0 {
		t.Fatalf("completed = %v, want none", spy.completed)
	}
}

// U4: --api-key with empty stdin emits started(api_key) then
// failed(api_key, api_key_invalid_input).
func TestRunAuthLogin_APIKeyEmptyInput_Telemetry(t *testing.T) {
	spy := withLoginAnalyticsSpy(t)
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())

	cmd, ctx := makeDispatchCmd(t)
	cmd.SetIn(strings.NewReader(""))

	if err := runAuthLogin(cmd, ctx, authLoginFlags{apiKeyMode: true}); err == nil {
		t.Fatal("want error, got nil")
	}

	if got := spy.started; len(got) != 1 || got[0] != "api_key" {
		t.Fatalf("started = %v, want [api_key]", got)
	}
	if got := spy.failed; len(got) != 1 || got[0] != (loginAnalyticsFailedCall{"api_key", "api_key_invalid_input"}) {
		t.Fatalf("failed = %v, want [{api_key api_key_invalid_input}]", got)
	}
}
