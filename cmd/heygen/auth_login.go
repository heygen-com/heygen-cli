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

	"github.com/heygen-com/heygen-cli/internal/auth"
	"github.com/heygen-com/heygen-cli/internal/auth/oauth"
	clierrors "github.com/heygen-com/heygen-cli/internal/errors"
	"github.com/heygen-com/heygen-cli/internal/paths"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

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

Single-credential normalization: a successful login clears the other
credential block (api_key or oauth) so the file holds at most one per
session. Pre-this-change users with both blocks self-heal on their
next login.`,
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
		return deps.runOAuth(cmd, ctx)
	case flags.apiKeyMode:
		return deps.runAPIKey(cmd, ctx)
	}

	// No explicit flag — pick a path from the environment. The picker
	// only makes sense when stdin AND stdout are real TTYs (otherwise
	// either the user can't see the menu or Bubble Tea can't read
	// keystrokes), and only when nothing in the environment has asked
	// us to behave non-interactively.
	if deps.stdinIsTerminal() && deps.stdoutIsTerminal() && !deps.nonInteractive() {
		choice, err := deps.runPicker(cmd.Context(), cmd.InOrStdin(), cmd.ErrOrStderr())
		if err != nil {
			if errors.Is(err, pickerCanceledError{}) || errorsAsPickerCanceled(err) {
				return newCanceledError()
			}
			return err
		}
		switch choice {
		case loginChoiceOAuth:
			return deps.runOAuth(cmd, ctx)
		case loginChoiceAPIKey:
			return deps.runAPIKey(cmd, ctx)
		default:
			return clierrors.New(fmt.Sprintf("unknown login choice: %d", choice))
		}
	}

	// Non-interactive shells default to the API-key flow per the team
	// decision: agents and CI runners feed keys on stdin, and the
	// browser-OAuth dance has nowhere to land its loopback callback
	// when there's no human to click "Allow".
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
func runAPIKeyLogin(cmd *cobra.Command, ctx *cmdContext) error {
	key, err := readAPIKey(cmd.InOrStdin(), cmd.ErrOrStderr())
	if err != nil {
		return err
	}
	if key == "" {
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

	credPath := filepath.Join(paths.ConfigDir(), "credentials")
	message := "API key saved to " + credPath
	if clearedOAuth {
		message += " (cleared previously-stored OAuth session)"
	}
	payload := map[string]any{
		"message":       message,
		"cleared_oauth": clearedOAuth,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return clierrors.New(fmt.Sprintf("failed to encode response: %v", err))
	}
	return ctx.formatter.Data(data, "", nil)
}

// runOAuthLogin drives the browser + PKCE + loopback dance and persists
// the resulting tokens.
func runOAuthLogin(cmd *cobra.Command, ctx *cmdContext, cfg oauthLoginConfig) error {
	// Headless-shell fast-fail: when explicit --oauth lands in a shell
	// with no TTY on stdin AND the environment opts out of opening a
	// browser (BROWSER=none or HEYGEN_NO_BROWSER=1), the callback can
	// never land. Without this we sit on the loopback for
	// DefaultLoopbackTimeout (~5min) before erroring out. Skip the
	// guard when cfg.OpenBrowser is injected (test path drives the
	// callback synthetically). (N1)
	if cfg.OpenBrowser == nil && isHeadlessOAuthShell() {
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
		return clierrors.New(fmt.Sprintf("oauth: timed out waiting for browser callback: %v", cmdCtx.Err()))
	case loopbackResult = <-results:
	}
	if loopbackResult.Err != nil {
		return clierrors.New(loopbackResult.Err.Error())
	}

	tok, err := oc.ExchangeAuthorizationCode(cmdCtx, loopbackResult.Code, verifier, loopbackResult.RedirectURI)
	if err != nil {
		return clierrors.New(fmt.Sprintf("oauth: token exchange failed: %v", err))
	}
	if tok.AccessToken == "" {
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
	username, email := lookupCurrentUser(cmdCtx, tok.AccessToken, probeBase)

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
	if username != "" {
		payload["username"] = username
	}
	if email != "" {
		payload["email"] = email
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return clierrors.New(fmt.Sprintf("failed to encode response: %v", err))
	}
	if username != "" {
		fmt.Fprintf(cmd.ErrOrStderr(), "Logged in as %s\n", username)
	} else if email != "" {
		fmt.Fprintf(cmd.ErrOrStderr(), "Logged in as %s\n", email)
	} else {
		fmt.Fprintln(cmd.ErrOrStderr(), "Logged in.")
	}
	return ctx.formatter.Data(data, "", nil)
}

// lookupCurrentUser GETs /v3/users/me with the freshly minted Bearer
// token and pulls a display name from the response. The CLI doesn't
// have the regular Client wired up yet at this point (we just minted
// the credential), so we issue a one-shot http call rather than
// re-bootstrapping the resolver.
func lookupCurrentUser(ctx context.Context, accessToken, baseURL string) (username, email string) {
	if baseURL == "" {
		baseURL = "https://api.heygen.com"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/v3/users/me", nil)
	if err != nil {
		return "", ""
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "heygen-cli/oauth-login")

	hc := &http.Client{Timeout: 10 * time.Second}
	resp, err := hc.Do(req) //nolint:gosec // G704: short-lived /v3/users/me probe
	if err != nil {
		return "", ""
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", ""
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", ""
	}
	var envelope struct {
		Data struct {
			Username string `json:"username"`
			Email    string `json:"email"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return "", ""
	}
	return envelope.Data.Username, envelope.Data.Email
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
