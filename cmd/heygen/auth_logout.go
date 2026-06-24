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
	cmd := &cobra.Command{
		Use:         "logout",
		Short:       "Clear the stored credential (api_key or OAuth) from disk",
		Annotations: map[string]string{"skipAuth": "true"},
		Long: `Clear the stored credential from ~/.heygen/credentials.

The credentials file holds at most one of api_key / oauth per session
(single-credential normalization on login). Logout clears whatever is
present.

For an OAuth session, the CLI first attempts a best-effort revoke
against the IdP so the refresh token can no longer mint new access
tokens. Network failures on the revoke step are ignored — the local
clear is authoritative.

If only HEYGEN_API_KEY is configured (no stored credentials), there is
nothing to log out of and the command is a no-op.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAuthLogout(cmd, ctx, defaultAuthLogoutConfig)
		},
	}
	return cmd
}

func runAuthLogout(cmd *cobra.Command, ctx *cmdContext, cfg authLogoutConfig) error {
	tok, err := auth.LoadOAuthTokens()
	var notConfigured *auth.ErrNotConfigured
	hadOAuthSession := err == nil
	if err != nil && !errors.As(err, &notConfigured) {
		return clierrors.New(fmt.Sprintf("failed to read credentials: %v", err))
	}

	// Whether an api_key block was on disk before clearing, used to
	// shape the user-facing message + JSON.
	hadAPIKey := false
	if k, kErr := auth.LoadAPIKeyFromFile(); kErr == nil && k != "" {
		hadAPIKey = true
	}

	if hadOAuthSession && tok.RefreshToken != "" {
		oc := oauth.NewClient(oauth.WithRevokeURL(cfg.RevokeURL))
		// Best-effort revoke; RevokeToken swallows network failures
		// internally (logout must not hang on a 5xx IdP).
		revokeCtx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
		_ = oc.RevokeToken(revokeCtx, tok.RefreshToken)
		cancel()
	}

	// Single-credential normalization is the new invariant — clear
	// whatever is on disk in one shot. Pre-this-change users may still
	// have both blocks; ClearOAuthTokens(true) wipes both, which is
	// the right semantic now ("there should only be one"). When the
	// file ends up empty it's removed.
	if err := auth.ClearOAuthTokens(true); err != nil {
		return clierrors.New(fmt.Sprintf("failed to clear credentials: %v", err))
	}

	clearedType := ""
	switch {
	case hadOAuthSession && hadAPIKey:
		clearedType = "both"
	case hadOAuthSession:
		clearedType = "oauth"
	case hadAPIKey:
		clearedType = "api_key"
	}

	var msg string
	switch {
	case hadOAuthSession:
		msg = "Logged out"
	case hadAPIKey:
		msg = "Cleared stored API key"
	default:
		msg = "No stored credentials"
	}
	data, err := json.Marshal(map[string]any{
		"message":            msg,
		"cleared_oauth":      hadOAuthSession,
		"cleared_api_key":    hadAPIKey,
		"cleared_credential": clearedType,
	})
	if err != nil {
		return clierrors.New(fmt.Sprintf("failed to encode response: %v", err))
	}
	fmt.Fprintln(cmd.ErrOrStderr(), msg+".")
	return ctx.formatter.Data(data, "", nil)
}
