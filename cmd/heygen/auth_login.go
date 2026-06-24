package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

func newAuthLoginCmd(ctx *cmdContext) *cobra.Command {
	var apiKeyMode bool
	var deviceCodeMode bool

	cmd := &cobra.Command{
		Use:         "login",
		Short:       "Log in via browser (OAuth) — or with --api-key to paste an API key",
		Annotations: map[string]string{"skipAuth": "true"},
		Long: `Authenticate the CLI against HeyGen.

Default (no flags): browser-based OAuth + PKCE flow. The CLI opens your
default browser to https://app.heygen.com/oauth/authorize and waits for
the redirect on a one-shot loopback HTTP server on 127.0.0.1.

Headless / SSH / CI: the browser cannot open, so the CLI prints the URL
for you to open elsewhere. Set BROWSER=none or HEYGEN_NO_BROWSER=1 to
force this behavior.

--api-key: skip OAuth and paste an API key from stdin (interactive or
piped). The key is stored at ~/.heygen/credentials with mode 0600.

The HEYGEN_API_KEY environment variable takes priority over any stored
credential when both are set.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if deviceCodeMode {
				return clierrors.NewUsage(
					"--device-code is not yet supported; use the default browser flow or --api-key",
				)
			}
			if apiKeyMode {
				return runAPIKeyLogin(cmd, ctx)
			}
			return runOAuthLogin(cmd, ctx, defaultOAuthLoginConfig)
		},
	}
	cmd.Flags().BoolVar(&apiKeyMode, "api-key", false, "Use API-key login (read key from stdin) instead of browser OAuth")
	cmd.Flags().BoolVar(&deviceCodeMode, "device-code", false, "Use device-code OAuth flow (not yet supported)")
	return cmd
}

// runAPIKeyLogin is the legacy stdin/prompt API-key flow, retained
// behind --api-key so existing automation keeps working unchanged.
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

	store := &auth.FileCredentialStore{}
	if err := store.Save(key); err != nil {
		return clierrors.New(fmt.Sprintf("failed to save credentials: %v", err))
	}

	credPath := filepath.Join(paths.ConfigDir(), "credentials")
	data, err := json.Marshal(map[string]string{
		"message": "API key saved to " + credPath,
	})
	if err != nil {
		return clierrors.New(fmt.Sprintf("failed to encode response: %v", err))
	}
	return ctx.formatter.Data(data, "", nil)
}

// runOAuthLogin drives the browser + PKCE + loopback dance and persists
// the resulting tokens.
func runOAuthLogin(cmd *cobra.Command, ctx *cmdContext, cfg oauthLoginConfig) error {
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

	authURL := oc.BuildAuthorizationURL(state, challenge, loopback.RedirectURI, "")

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
	payload := map[string]any{
		"message":    "Signed in via OAuth; credentials saved to " + credPath,
		"expires_at": "",
		"scope":      tok.Scope,
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
