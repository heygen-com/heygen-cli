package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/heygen-com/heygen-cli/internal/analytics"
	"github.com/heygen-com/heygen-cli/internal/auth"
	"github.com/heygen-com/heygen-cli/internal/auth/oauth"
	clierrors "github.com/heygen-com/heygen-cli/internal/errors"
	"github.com/heygen-com/heygen-cli/internal/paths"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// loginAnalyticsClient is the subset of *analytics.Client this file calls,
// factored out as an interface so tests can inject a spy without needing a
// real PostHog capture client (internal/analytics's own stubCaptureClient
// is the lower-level equivalent used in that package's tests).
type loginAnalyticsClient interface {
	IdentifyAccount(distinctId string)
	AuthLoginStarted(method string)
	AuthLoginCompleted(method string)
	AuthLoginFailed(method, reason string)
}

// loginAnalytics is the process-wide analytics client login telemetry
// calls into. main() sets it to the real client once at startup; left at
// its inert zero value here so any test that builds the login commands
// directly (bypassing main()) gets a no-op, matching analytics.New(_,
// false)'s existing disabled-client contract.
var loginAnalytics loginAnalyticsClient = &analytics.Client{}

// identityKey picks the identity a successful login links analytics to:
// email if present, otherwise username. Deliberately narrower than
// UserInfo.DisplayName() (which also falls back to "first last") — this
// links a PostHog person to a stable login handle, not a display string.
// Lowercased so this joins with hyperframes-oss's own identify call
// regardless of the account's stored casing — an uppercase-email user would
// otherwise split across two PostHog profiles.
func identityKey(userInfo auth.UserInfo) string {
	if userInfo.Email != "" {
		return strings.ToLower(userInfo.Email)
	}
	return strings.ToLower(userInfo.Username)
}

// oauthLoginConfig is overridable in tests so the login flow can be
// driven against an httptest IdP without spawning a real browser.
type oauthLoginConfig struct {
	// Endpoints
	AuthorizeURL string
	TokenURL     string

	// Behavior overrides (tests pin these)
	OpenBrowser    func(authURL string) error
	UsersMeBaseURL string
	Now            func() time.Time
}

var defaultOAuthLoginConfig = oauthLoginConfig{
	AuthorizeURL: oauth.DefaultAuthorizeURL,
	TokenURL:     oauth.DefaultTokenURL,
}

// apiKeyLoginConfig is overridable in tests so the api-key login flow's
// friendly-display probe can be driven against an httptest /v3/users/me
// without depending on the production base URL. Production callers
// leave every field zero — the dispatcher reads ctx.configProvider for
// the base URL the same way oauth login does.
type apiKeyLoginConfig struct {
	// UsersMeBaseURL pins the /v3/users/me base URL (test-only). When
	// empty, the dispatcher falls back to ctx.configProvider.BaseURL().
	UsersMeBaseURL string
}

// apiKeyLoginConfigForTest is set by tests that need to override the
// friendly-display probe target. Production callers leave this nil so
// runAPIKeyLogin walks the standard ctx.configProvider path.
var apiKeyLoginConfigForTest *apiKeyLoginConfig

// authLoginRedirectPath is the loopback path the browser callback hits.
const authLoginRedirectPath = "/oauth/callback"

// stdoutIsTerminalFunc and nonInteractiveEnvFunc are overridable in
// tests so the dispatch logic in runAuthLogin can be exercised without
// a real TTY or having to mutate the process environment.
var (
	stdoutIsTerminalFunc = func() bool {
		return term.IsTerminal(int(os.Stdout.Fd()))
	}
	// nonInteractiveEnvFunc reports whether the environment is asking
	// us to skip the picker even on a TTY (CI runners, agent shells
	// that wrap our stdin but still expose a tty, etc).
	nonInteractiveEnvFunc = func() bool {
		if v := strings.TrimSpace(os.Getenv("HEYGEN_NONINTERACTIVE")); v != "" && v != "0" && !strings.EqualFold(v, "false") {
			return true
		}
		if v := strings.TrimSpace(os.Getenv("CI")); v != "" && v != "0" && !strings.EqualFold(v, "false") {
			return true
		}
		return false
	}
)

// isHeadlessOAuthShell reports whether the current shell is incapable
// of completing a browser OAuth flow — no TTY on stdin AND an
// environment opt-out from opening a browser. Used by runOAuthLogin to
// fast-fail explicit `--oauth` instead of blocking ~5min on the
// loopback timeout. (N1)
func isHeadlessOAuthShell() bool {
	if stdinIsTerminalFunc() {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(os.Getenv("BROWSER")), "none") {
		return true
	}
	v := strings.TrimSpace(os.Getenv("HEYGEN_NO_BROWSER"))
	if v != "" && v != "0" && !strings.EqualFold(v, "false") {
		return true
	}
	return false
}

func newAuthLoginCmd(ctx *cmdContext) *cobra.Command {
	var apiKeyMode bool
	var oauthMode bool
	var deviceCodeMode bool

	cmd := &cobra.Command{
		Use:         "login",
		Short:       "Log in to HeyGen (interactive picker; --oauth / --api-key to skip)",
		Annotations: map[string]string{"skipAuth": "true"},
		Long: `Authenticate the CLI against HeyGen.

Interactive shells (stdin + stdout are both TTYs): an interactive
picker offers two options, defaulted to the API-key path:

  • Use an API key (paste an existing key — uses API credits)
  • Login with HeyGen.com (browser OAuth — uses subscription credits)

Non-interactive shells (piped stdin/stdout, CI=true, or
HEYGEN_NONINTERACTIVE=1): skips the picker and runs the API-key flow
so unattended agents and scripts keep working unchanged.

Flags skip the picker:
  --api-key   Read an API key from stdin (interactive prompt or pipe)
  --oauth     Start the browser OAuth flow directly

The OAuth flow opens your default browser to
https://app.heygen.com/oauth/authorize and waits for the redirect on a
one-shot loopback HTTP server on 127.0.0.1. Set BROWSER=none or
HEYGEN_NO_BROWSER=1 to print the URL instead of opening it.

The API-key flow stores the key at ~/.heygen/credentials with mode
0600. The HEYGEN_API_KEY environment variable takes priority over any
stored credential when both are set.

IMPORTANT — running auth login REPLACES the stored credential:

  • Logging in with an API key clears any stored OAuth session.
  • Logging in with OAuth clears any stored API key.

The credentials file holds at most ONE of api_key / oauth at any
time. heygen auth status reports which is active.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAuthLogin(cmd, ctx, authLoginFlags{
				apiKeyMode:     apiKeyMode,
				oauthMode:      oauthMode,
				deviceCodeMode: deviceCodeMode,
			})
		},
	}
	cmd.Flags().BoolVar(&apiKeyMode, "api-key", false, "Skip the picker and use API-key login (read key from stdin)")
	cmd.Flags().BoolVar(&oauthMode, "oauth", false, "Skip the picker and start the browser OAuth flow")
	cmd.Flags().BoolVar(&deviceCodeMode, "device-code", false, "Use device-code OAuth flow (not yet supported)")
	cmd.MarkFlagsMutuallyExclusive("api-key", "oauth")
	return cmd
}

// authLoginFlags packages the user-facing flag selections so the
// dispatcher (runAuthLogin) is straightforward to unit-test without
// having to build a full cobra command tree.
type authLoginFlags struct {
	apiKeyMode     bool
	oauthMode      bool
	deviceCodeMode bool
}

// runAuthLoginDeps wires runtime dependencies that we want to swap
// out in tests — TTY detection, environment-based non-interactive
// overrides, the picker entry point, and the two backing runners.
// Production callers leave every field zero/nil and we fall back to
// the package-level defaults.
type runAuthLoginDeps struct {
	stdinIsTerminal  func() bool
	stdoutIsTerminal func() bool
	nonInteractive   func() bool
	runPicker        func(ctx context.Context, stdin io.Reader, stderr io.Writer) (loginChoice, error)
	runOAuth         func(cmd *cobra.Command, ctx *cmdContext) error
	runAPIKey        func(cmd *cobra.Command, ctx *cmdContext) error
}

// runAuthLoginTestDeps lets tests inject a fully stubbed dependency
// bundle for runAuthLogin without going through the package-level
// dispatch defaults. Left nil in production.
var runAuthLoginTestDeps *runAuthLoginDeps

// runAuthLogin parses the flags and the environment to decide which
// login flow to run. Explicit flags always win; otherwise we show the
// picker if both stdin and stdout are TTYs AND no non-interactive
// override is in effect, else we default to the API-key flow.
func runAuthLogin(cmd *cobra.Command, ctx *cmdContext, flags authLoginFlags) error {
	if flags.deviceCodeMode {
		loginAnalytics.AuthLoginStarted("device_code")
		loginAnalytics.AuthLoginFailed("device_code", "device_code_unsupported")
		return clierrors.NewUsage(
			"--device-code is not yet supported; use the default browser flow or --api-key",
		)
	}

	deps := runAuthLoginDeps{}
	if runAuthLoginTestDeps != nil {
		deps = *runAuthLoginTestDeps
	}
	if deps.stdinIsTerminal == nil {
		deps.stdinIsTerminal = stdinIsTerminalFunc
	}
	if deps.stdoutIsTerminal == nil {
		deps.stdoutIsTerminal = stdoutIsTerminalFunc
	}
	if deps.nonInteractive == nil {
		deps.nonInteractive = nonInteractiveEnvFunc
	}
	if deps.runPicker == nil {
		deps.runPicker = runPickerFunc
	}
	if deps.runOAuth == nil {
		deps.runOAuth = func(c *cobra.Command, x *cmdContext) error {
			return runOAuthLogin(c, x, defaultOAuthLoginConfig)
		}
	}
	if deps.runAPIKey == nil {
		deps.runAPIKey = runAPIKeyLogin
	}

	switch {
	case flags.oauthMode:
		loginAnalytics.AuthLoginStarted("oauth")
		return deps.runOAuth(cmd, ctx)
	case flags.apiKeyMode:
		loginAnalytics.AuthLoginStarted("api_key")
		return deps.runAPIKey(cmd, ctx)
	}

	// No explicit flag — pick a path from the environment. The picker
	// only makes sense when stdin AND stdout are real TTYs (otherwise
	// either the user can't see the menu or Bubble Tea can't read
	// keystrokes), and only when nothing in the environment has asked
	// us to behave non-interactively.
	if deps.stdinIsTerminal() && deps.stdoutIsTerminal() && !deps.nonInteractive() {
		// No AuthLoginStarted here — the picker hasn't resolved a method
		// yet. A cancel below must emit no started/failed pair at all.
		choice, err := deps.runPicker(cmd.Context(), cmd.InOrStdin(), cmd.ErrOrStderr())
		if err != nil {
			if errors.Is(err, pickerCanceledError{}) || errorsAsPickerCanceled(err) {
				return newCanceledError()
			}
			return err
		}
		switch choice {
		case loginChoiceOAuth:
			loginAnalytics.AuthLoginStarted("oauth")
			return deps.runOAuth(cmd, ctx)
		case loginChoiceAPIKey:
			loginAnalytics.AuthLoginStarted("api_key")
			return deps.runAPIKey(cmd, ctx)
		default:
			return clierrors.New(fmt.Sprintf("unknown login choice: %d", choice))
		}
	}

	// Non-interactive shells default to the API-key flow per the team
	// decision: agents and CI runners feed keys on stdin, and the
	// browser-OAuth dance has nowhere to land its loopback callback
	// when there's no human to click "Allow".
	loginAnalytics.AuthLoginStarted("api_key")
	return deps.runAPIKey(cmd, ctx)
}

// errorsAsPickerCanceled lets us treat a pickerCanceledError that has
// been wrapped (e.g. by errors.New) the same as a direct sentinel
// match. The picker itself returns the bare value, but defensive code
// downstream may wrap it.
func errorsAsPickerCanceled(err error) bool {
	var target pickerCanceledError
	return errors.As(err, &target)
}

// runAPIKeyLogin is the legacy stdin/prompt API-key flow, retained
// behind --api-key so existing automation keeps working unchanged.
//
// On success, any co-located OAuth block from a prior session is
// cleared so the file holds at most one of api_key / oauth (the
// single-credential normalization invariant). Pre-this-change users
// with both blocks self-heal on their next login.
func runAPIKeyLogin(cmd *cobra.Command, ctx *cmdContext) (err error) {
	// reported tracks whether this attempt already emitted a terminal
	// analytics event (AuthLoginCompleted/AuthLoginFailed) on one of the
	// explicitly-instrumented paths below. The deferred check is a
	// safety net for every OTHER error return in this function (e.g. a
	// future early return nobody remembers to instrument): without it,
	// the AuthLoginStarted event the caller already fired would never
	// reconcile.
	reported := false
	defer func() {
		if err != nil && !reported {
			loginAnalytics.AuthLoginFailed("api_key", "internal_error")
		}
	}()

	key, err := readAPIKey(cmd.InOrStdin(), cmd.ErrOrStderr())
	if err != nil {
		reported = true
		loginAnalytics.AuthLoginFailed("api_key", "api_key_aborted")
		return err
	}
	if key == "" {
		reported = true
		loginAnalytics.AuthLoginFailed("api_key", "api_key_invalid_input")
		if stdinIsTerminalFunc() {
			return clierrors.NewUsage(
				"no API key entered — type your key after the prompt, or paste it.",
			)
		}
		return clierrors.NewUsage(
			"no API key provided on stdin.\n" +
				"Pipe your key:  echo \"$KEY\" | heygen auth login --api-key\n" +
				"Or set the HEYGEN_API_KEY environment variable.",
		)
	}

	// Detect a pre-existing OAuth block before save so we can vary the
	// success message. LoadOAuthTokens returns ErrNotConfigured when no
	// oauth block is present; any other error (e.g. malformed file) we
	// surface immediately so we don't clobber a recoverable session.
	hadOAuth := false
	if _, loadErr := auth.LoadOAuthTokens(); loadErr == nil {
		hadOAuth = true
	} else {
		var notConfigured *auth.ErrNotConfigured
		if !errors.As(loadErr, &notConfigured) {
			return clierrors.New(fmt.Sprintf("failed to read credentials: %v", loadErr))
		}
	}

	store := &auth.FileCredentialStore{}
	if err := store.Save(key); err != nil {
		return clierrors.New(fmt.Sprintf("failed to save credentials: %v", err))
	}

	// Single-credential normalization: drop any stale OAuth block so the
	// file holds at most one of api_key / oauth. ClearOAuthTokens(false)
	// only clears the oauth block; the api_key we just saved is left in
	// place. A missing file is a no-op (treated as already-normalized).
	clearedOAuth := false
	if hadOAuth {
		if err := auth.ClearOAuthTokens(false); err != nil {
			return clierrors.New(fmt.Sprintf("failed to clear previous OAuth session: %v", err))
		}
		clearedOAuth = true
	}

	// Best-effort identity probe so we can surface "Logged in as ..."
	// for api-key callers too. Same contract as the OAuth probe — a
	// failure here is non-fatal; the api_key on disk is still usable,
	// and the user just won't see friendly fields until the next
	// successful re-login.
	//
	// Resolve the base URL the same way the OAuth flow does so a CLI
	// pointed at a dev sandbox via HEYGEN_API_BASE doesn't accidentally
	// hit production for the probe.
	probeBase := ""
	if apiKeyLoginConfigForTest != nil {
		probeBase = apiKeyLoginConfigForTest.UsersMeBaseURL
	}
	if probeBase == "" && ctx.configProvider != nil {
		probeBase = ctx.configProvider.BaseURL()
	}
	userInfo := lookupCurrentUserAPIKey(cmd.Context(), key, probeBase)
	if !userInfo.IsZero() {
		if saveErr := auth.SaveUserInfo(userInfo); saveErr != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "(warning: could not persist user info: %v)\n", saveErr)
		}
		if id := identityKey(userInfo); id != "" {
			loginAnalytics.IdentifyAccount(id)
		}
	} else if clearErr := auth.ClearUserInfo(); clearErr != nil {
		// Probe failed but a stale user block from a prior login may
		// still be on disk. Best-effort clear; a failure here is
		// non-fatal (stale display is a minor UX bug, not security).
		fmt.Fprintf(cmd.ErrOrStderr(), "(warning: could not clear stale user info: %v)\n", clearErr)
	}

	credPath := filepath.Join(paths.ConfigDir(), "credentials")
	message := "API key saved to " + credPath
	if clearedOAuth {
		message += " (cleared previously-stored OAuth session)"
	}
	payload := map[string]any{
		"message":       message,
		"cleared_oauth": clearedOAuth,
	}
	if userInfo.Username != "" {
		payload["username"] = userInfo.Username
	}
	if userInfo.Email != "" {
		payload["email"] = userInfo.Email
	}
	if userInfo.FirstName != "" {
		payload["first_name"] = userInfo.FirstName
	}
	if userInfo.LastName != "" {
		payload["last_name"] = userInfo.LastName
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return clierrors.New(fmt.Sprintf("failed to encode response: %v", err))
	}
	if display := userInfo.DisplayName(); display != "" {
		fmt.Fprintf(cmd.ErrOrStderr(), "Logged in as %s\n", display)
	}
	// Only report the login as completed once the response has actually
	// been written out successfully: if the formatter fails (e.g. a
	// broken pipe on stdout), the command still exits non-zero and the
	// deferred check above reports AuthLoginFailed instead.
	if err = ctx.formatter.Data(data, "", nil); err != nil {
		return err
	}
	reported = true
	loginAnalytics.AuthLoginCompleted("api_key")
	return nil
}

// runOAuthLogin drives the browser + PKCE + loopback dance and persists
// the resulting tokens.
func runOAuthLogin(cmd *cobra.Command, ctx *cmdContext, cfg oauthLoginConfig) (err error) {
	// reported tracks whether this attempt already emitted a terminal
	// analytics event (AuthLoginCompleted/AuthLoginFailed) on one of the
	// explicitly-instrumented paths below. The deferred check is a
	// safety net for every OTHER error return in this function (e.g. a
	// future early return nobody remembers to instrument): without it,
	// the AuthLoginStarted event the caller already fired would never
	// reconcile.
	reported := false
	defer func() {
		if err != nil && !reported {
			loginAnalytics.AuthLoginFailed("oauth", "internal_error")
		}
	}()

	// Headless-shell fast-fail: when explicit --oauth lands in a shell
	// with no TTY on stdin AND the environment opts out of opening a
	// browser (BROWSER=none or HEYGEN_NO_BROWSER=1), the callback can
	// never land. Without this we sit on the loopback for
	// DefaultLoopbackTimeout (~5min) before erroring out. Skip the
	// guard when cfg.OpenBrowser is injected (test path drives the
	// callback synthetically). (N1)
	if cfg.OpenBrowser == nil && isHeadlessOAuthShell() {
		reported = true
		loginAnalytics.AuthLoginFailed("oauth", "headless_shell")
		return clierrors.NewUsage(
			"cannot complete browser OAuth flow in a headless shell " +
				"(no TTY and BROWSER=none / HEYGEN_NO_BROWSER=1).\n" +
				"Use `heygen auth login --api-key` instead, or unset " +
				"HEYGEN_NO_BROWSER and re-run from an interactive shell.",
		)
	}

	cmdCtx, cancel := context.WithTimeout(cmd.Context(), oauth.DefaultLoopbackTimeout+30*time.Second)
	defer cancel()

	verifier, challenge, err := oauth.GeneratePKCEPair()
	if err != nil {
		return clierrors.New(fmt.Sprintf("oauth: PKCE: %v", err))
	}
	state, err := oauth.GenerateState()
	if err != nil {
		return clierrors.New(fmt.Sprintf("oauth: state: %v", err))
	}

	loopback, results, stopLoopback, err := oauth.StartLoopbackServer(cmdCtx, authLoginRedirectPath, state)
	if err != nil {
		return clierrors.New(fmt.Sprintf("oauth: loopback: %v", err))
	}
	defer stopLoopback()

	clientOpts := []oauth.Option{}
	if cfg.AuthorizeURL != "" {
		clientOpts = append(clientOpts, oauth.WithAuthorizeURL(cfg.AuthorizeURL))
	}
	if cfg.TokenURL != "" {
		clientOpts = append(clientOpts, oauth.WithTokenURL(cfg.TokenURL))
	}
	if cfg.Now != nil {
		clientOpts = append(clientOpts, oauth.WithNow(cfg.Now))
	}
	oc := oauth.NewClient(clientOpts...)

	authURL, err := oc.BuildAuthorizationURL(state, challenge, loopback.RedirectURI, "")
	if err != nil {
		return clierrors.New(fmt.Sprintf("oauth: build authorize URL: %v", err))
	}

	fmt.Fprintln(cmd.ErrOrStderr(), "Opening browser to https://app.heygen.com/oauth/authorize ...")
	openFn := cfg.OpenBrowser
	if openFn == nil {
		openFn = oauth.OpenBrowser
	}
	if err := openFn(authURL); err != nil {
		// Not fatal — printManualURL inside OpenBrowser already printed
		// a fallback. Log and continue waiting for the callback.
		fmt.Fprintf(cmd.ErrOrStderr(), "(could not open browser automatically: %v)\n", err)
	}

	var loopbackResult oauth.LoopbackResult
	select {
	case <-cmdCtx.Done():
		reported = true
		loginAnalytics.AuthLoginFailed("oauth", "oauth_timeout")
		return clierrors.New(fmt.Sprintf("oauth: timed out waiting for browser callback: %v", cmdCtx.Err()))
	case loopbackResult = <-results:
	}
	if loopbackResult.Err != nil {
		reported = true
		loginAnalytics.AuthLoginFailed("oauth", "oauth_flow_error")
		return clierrors.New(loopbackResult.Err.Error())
	}

	tok, err := oc.ExchangeAuthorizationCode(cmdCtx, loopbackResult.Code, verifier, loopbackResult.RedirectURI)
	if err != nil {
		reported = true
		loginAnalytics.AuthLoginFailed("oauth", "token_exchange_failed")
		return clierrors.New(fmt.Sprintf("oauth: token exchange failed: %v", err))
	}
	if tok.AccessToken == "" {
		reported = true
		loginAnalytics.AuthLoginFailed("oauth", "token_exchange_failed")
		return clierrors.New("oauth: token endpoint returned no access_token")
	}

	expiresAt := time.Time{}
	if tok.ExpiresIn > 0 {
		expiresAt = tok.IssuedAt.Add(time.Duration(tok.ExpiresIn) * time.Second)
	}
	if err := auth.SaveOAuthTokens(auth.OAuthTokens{
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		ExpiresAt:    expiresAt,
		Scope:        tok.Scope,
		TokenType:    tok.TokenType,
	}); err != nil {
		return clierrors.New(fmt.Sprintf("failed to save credentials: %v", err))
	}

	// Single-credential normalization: drop any stale api_key block so
	// the file holds at most one of api_key / oauth. ClearAPIKey
	// reports whether one was actually cleared so the success message
	// can mention it.
	clearedAPIKey, err := auth.ClearAPIKey()
	if err != nil {
		return clierrors.New(fmt.Sprintf("failed to clear previous API key: %v", err))
	}

	// Best-effort identity probe so we can surface "Logged in as ...".
	// A failure here doesn't roll back the login (tokens are on disk and
	// usable); we just leave the identity blank in the success payload.
	//
	// Resolve the base URL in priority order: test-pinned override
	// (cfg.UsersMeBaseURL) > the CLI's config provider (which honors
	// HEYGEN_API_BASE) > a hardcoded fallback inside lookupCurrentUser.
	// (W4 — without this the identity probe hits api.heygen.com even
	// when the user pointed the CLI at a dev sandbox.)
	probeBase := cfg.UsersMeBaseURL
	if probeBase == "" && ctx.configProvider != nil {
		probeBase = ctx.configProvider.BaseURL()
	}
	userInfo := lookupCurrentUser(cmdCtx, tok.AccessToken, probeBase)

	// Persist the friendly-display block alongside the OAuth tokens so
	// subsequent `auth status` invocations can show "Logged in as ..."
	// without re-hitting /v3/users/me. Best-effort: a persist failure is
	// non-fatal (login proceeds; the user just won't see friendly fields
	// until the next successful re-login).
	if !userInfo.IsZero() {
		if saveErr := auth.SaveUserInfo(userInfo); saveErr != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "(warning: could not persist user info: %v)\n", saveErr)
		}
		if id := identityKey(userInfo); id != "" {
			loginAnalytics.IdentifyAccount(id)
		}
	} else if clearErr := auth.ClearUserInfo(); clearErr != nil {
		// Probe failed AND the file might still hold a stale user block
		// from a prior login (different account). Best-effort clear; a
		// failure here is non-fatal — stale display is a minor UX bug,
		// not a security issue.
		fmt.Fprintf(cmd.ErrOrStderr(), "(warning: could not clear stale user info: %v)\n", clearErr)
	}

	credPath := filepath.Join(paths.ConfigDir(), "credentials")
	message := "Signed in via OAuth; credentials saved to " + credPath
	if clearedAPIKey {
		message += " (cleared previously-stored API key)"
	}
	payload := map[string]any{
		"message":         message,
		"expires_at":      "",
		"scope":           tok.Scope,
		"cleared_api_key": clearedAPIKey,
	}
	if !expiresAt.IsZero() {
		payload["expires_at"] = expiresAt.UTC().Format(time.RFC3339)
	}
	if userInfo.Username != "" {
		payload["username"] = userInfo.Username
	}
	if userInfo.Email != "" {
		payload["email"] = userInfo.Email
	}
	if userInfo.FirstName != "" {
		payload["first_name"] = userInfo.FirstName
	}
	if userInfo.LastName != "" {
		payload["last_name"] = userInfo.LastName
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return clierrors.New(fmt.Sprintf("failed to encode response: %v", err))
	}
	if display := userInfo.DisplayName(); display != "" {
		fmt.Fprintf(cmd.ErrOrStderr(), "Logged in as %s\n", display)
	} else {
		fmt.Fprintln(cmd.ErrOrStderr(), "Logged in.")
	}
	// Only report the login as completed once the response has actually
	// been written out successfully: if the formatter fails (e.g. a
	// broken pipe on stdout), the command still exits non-zero and the
	// deferred check above reports AuthLoginFailed instead.
	if err = ctx.formatter.Data(data, "", nil); err != nil {
		return err
	}
	reported = true
	loginAnalytics.AuthLoginCompleted("oauth")
	return nil
}

// lookupCurrentUser GETs /v3/users/me with the freshly minted Bearer
// token and pulls the friendly-display fields from the response. The
// CLI doesn't have the regular Client wired up yet at this point (we
// just minted the credential), so we issue a one-shot http call rather
// than re-bootstrapping the resolver.
//
// On any failure (network, non-200, malformed body) returns a zero
// UserInfo with no error — the caller treats this as "friendly display
// unavailable" and proceeds without it. The login itself is NEVER
// rolled back on a probe failure.
func lookupCurrentUser(ctx context.Context, accessToken, baseURL string) auth.UserInfo {
	req, err := buildUsersMeRequest(ctx, baseURL)
	if err != nil {
		return auth.UserInfo{}
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("User-Agent", "heygen-cli/oauth-login")
	return runUsersMeRequest(req)
}

// lookupCurrentUserAPIKey is the api_key equivalent of
// lookupCurrentUser. It sends the key on the x-api-key header (the
// transport that the api_key resolver would use) and pulls the same
// friendly-display fields. Same best-effort contract — a failure here
// is non-fatal and yields a zero UserInfo.
func lookupCurrentUserAPIKey(ctx context.Context, apiKey, baseURL string) auth.UserInfo {
	req, err := buildUsersMeRequest(ctx, baseURL)
	if err != nil {
		return auth.UserInfo{}
	}
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("User-Agent", "heygen-cli/apikey-login")
	return runUsersMeRequest(req)
}

// buildUsersMeRequest assembles the GET /v3/users/me request shared by
// both lookup paths. Centralized so the OAuth and api_key variants
// can't drift on URL / headers / context plumbing.
func buildUsersMeRequest(ctx context.Context, baseURL string) (*http.Request, error) {
	if baseURL == "" {
		baseURL = "https://api.heygen.com"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/v3/users/me", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	return req, nil
}

// runUsersMeRequest executes the prepared /v3/users/me probe and
// decodes the friendly-display fields. Returns a zero UserInfo on any
// failure (network, non-200, malformed body) — the caller treats this
// as "friendly display unavailable" and proceeds without it.
func runUsersMeRequest(req *http.Request) auth.UserInfo {
	hc := &http.Client{Timeout: 10 * time.Second}
	resp, err := hc.Do(req) //nolint:gosec // G704: short-lived /v3/users/me probe
	if err != nil {
		return auth.UserInfo{}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return auth.UserInfo{}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return auth.UserInfo{}
	}
	var envelope struct {
		Data struct {
			Username  string `json:"username"`
			Email     string `json:"email"`
			FirstName string `json:"first_name"`
			LastName  string `json:"last_name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return auth.UserInfo{}
	}
	return auth.UserInfo{
		Username:  envelope.Data.Username,
		Email:     envelope.Data.Email,
		FirstName: envelope.Data.FirstName,
		LastName:  envelope.Data.LastName,
	}
}

func readAPIKey(in io.Reader, errOut io.Writer) (string, error) {
	if file, ok := in.(interface{ Fd() uintptr }); ok && term.IsTerminal(int(file.Fd())) {
		if _, err := fmt.Fprint(errOut, "Enter API key: "); err != nil {
			return "", clierrors.New(fmt.Sprintf("failed to write prompt: %v", err))
		}

		raw, err := term.ReadPassword(int(file.Fd()))
		if _, writeErr := fmt.Fprintln(errOut); writeErr != nil && err == nil {
			err = writeErr
		}
		if err != nil {
			return "", clierrors.New(fmt.Sprintf("failed to read input: %v", err))
		}

		return strings.TrimSpace(string(raw)), nil
	}

	data, err := io.ReadAll(in)
	if err != nil {
		return "", clierrors.New(fmt.Sprintf("failed to read stdin: %v", err))
	}
	return strings.TrimSpace(string(data)), nil
}
