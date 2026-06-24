package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/heygen-com/heygen-cli/internal/auth"
	"github.com/heygen-com/heygen-cli/internal/auth/oauth"
	clierrors "github.com/heygen-com/heygen-cli/internal/errors"
	"github.com/spf13/cobra"
)

// authLogoutConfig is overridable in tests so a fake IdP can absorb the
// best-effort revoke without spawning network requests.
type authLogoutConfig struct {
	RevokeURL string
}

var defaultAuthLogoutConfig = authLogoutConfig{
	RevokeURL: oauth.DefaultRevokeURL,
}

func newAuthLogoutCmd(ctx *cmdContext) *cobra.Command {
	var alsoAPIKey bool

	cmd := &cobra.Command{
		Use:         "logout",
		Short:       "Clear the stored OAuth session (best-effort server revoke)",
		Annotations: map[string]string{"skipAuth": "true"},
		Long: `Clear the OAuth session from ~/.heygen/credentials.

The CLI first attempts a best-effort revoke against the IdP so the
refresh token can no longer mint new access tokens. Network failures on
the revoke step are ignored — the local clear is authoritative.

A co-located api_key is left in place by default. Pass --all to also
clear the api_key block.

If only HEYGEN_API_KEY is configured (no stored credentials), there is
nothing to log out of and the command is a no-op.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAuthLogout(cmd, ctx, alsoAPIKey, defaultAuthLogoutConfig)
		},
	}
	cmd.Flags().BoolVar(&alsoAPIKey, "all", false, "Also clear the api_key block from the credentials file")
	return cmd
}

func runAuthLogout(cmd *cobra.Command, ctx *cmdContext, alsoAPIKey bool, cfg authLogoutConfig) error {
	tok, err := auth.LoadOAuthTokens()
	var notConfigured *auth.ErrNotConfigured
	hadOAuthSession := err == nil
	if err != nil && !errors.As(err, &notConfigured) {
		return clierrors.New(fmt.Sprintf("failed to read credentials: %v", err))
	}

	if hadOAuthSession && tok.RefreshToken != "" {
		oc := oauth.NewClient(oauth.WithRevokeURL(cfg.RevokeURL))
		// Best-effort revoke; RevokeToken swallows network failures
		// internally (logout must not hang on a 5xx IdP).
		revokeCtx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
		_ = oc.RevokeToken(revokeCtx, tok.RefreshToken)
		cancel()
	}

	if err := auth.ClearOAuthTokens(alsoAPIKey); err != nil {
		return clierrors.New(fmt.Sprintf("failed to clear credentials: %v", err))
	}

	msg := "Logged out"
	if !hadOAuthSession {
		msg = "No OAuth session was stored"
	}
	data, err := json.Marshal(map[string]any{
		"message":                 msg,
		"cleared_oauth":           hadOAuthSession,
		"clear_api_key_requested": alsoAPIKey,
	})
	if err != nil {
		return clierrors.New(fmt.Sprintf("failed to encode response: %v", err))
	}
	fmt.Fprintln(cmd.ErrOrStderr(), msg+".")
	return ctx.formatter.Data(data, "", nil)
}
