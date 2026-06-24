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

// authRefreshConfig is overridable in tests.
type authRefreshConfig struct {
	TokenURL string
}

var defaultAuthRefreshConfig = authRefreshConfig{
	TokenURL: oauth.DefaultTokenURL,
}

func newAuthRefreshCmd(ctx *cmdContext) *cobra.Command {
	return &cobra.Command{
		Use:         "refresh",
		Short:       "Refresh the stored OAuth access token",
		Annotations: map[string]string{"skipAuth": "true"},
		Long: `Refresh the OAuth access token using the stored refresh token, then
persist the new tokens back to ~/.heygen/credentials.

Errors with exit code 3 if no OAuth session is stored (run
heygen auth login first).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAuthRefresh(cmd, ctx, defaultAuthRefreshConfig)
		},
	}
}

func runAuthRefresh(cmd *cobra.Command, ctx *cmdContext, cfg authRefreshConfig) error {
	tok, err := auth.LoadOAuthTokens()
	if err != nil {
		var notConfigured *auth.ErrNotConfigured
		if errors.As(err, &notConfigured) {
			return clierrors.NewAuth(
				"not logged in via OAuth",
				"Run: heygen auth login",
			)
		}
		return clierrors.New(fmt.Sprintf("failed to read credentials: %v", err))
	}
	if tok.RefreshToken == "" {
		return clierrors.NewAuth(
			"no refresh_token stored",
			"Run: heygen auth login",
		)
	}

	oc := oauth.NewClient(oauth.WithTokenURL(cfg.TokenURL))
	refreshCtx, cancel := context.WithTimeout(cmd.Context(), oauth.DefaultExchangeTimeout)
	defer cancel()

	resp, err := oc.RefreshAccessToken(refreshCtx, tok.RefreshToken)
	if err != nil {
		if errors.Is(err, oauth.ErrRejected) {
			return clierrors.NewAuth(
				"refresh token rejected by IdP",
				"Run: heygen auth login",
			)
		}
		return clierrors.New(fmt.Sprintf("oauth: refresh failed: %v", err))
	}

	expiresAt := time.Time{}
	if resp.ExpiresIn > 0 {
		expiresAt = resp.IssuedAt.Add(time.Duration(resp.ExpiresIn) * time.Second)
	}
	newRefresh := tok.RefreshToken
	if resp.RefreshToken != "" {
		newRefresh = resp.RefreshToken
	}
	newScope := tok.Scope
	if resp.Scope != "" {
		newScope = resp.Scope
	}
	newTokenType := tok.TokenType
	if resp.TokenType != "" {
		newTokenType = resp.TokenType
	}
	if err := auth.SaveOAuthTokens(auth.OAuthTokens{
		AccessToken:  resp.AccessToken,
		RefreshToken: newRefresh,
		ExpiresAt:    expiresAt,
		Scope:        newScope,
		TokenType:    newTokenType,
	}); err != nil {
		return clierrors.New(fmt.Sprintf("failed to save refreshed credentials: %v", err))
	}

	payload := map[string]any{
		"message": "Refreshed.",
		"scope":   newScope,
	}
	if !expiresAt.IsZero() {
		payload["expires_at"] = expiresAt.UTC().Format(time.RFC3339)
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return clierrors.New(fmt.Sprintf("failed to encode response: %v", err))
	}
	if !expiresAt.IsZero() {
		fmt.Fprintf(cmd.ErrOrStderr(), "Refreshed. Expires at %s.\n", expiresAt.UTC().Format(time.RFC3339))
	} else {
		fmt.Fprintln(cmd.ErrOrStderr(), "Refreshed.")
	}
	return ctx.formatter.Data(data, "", nil)
}
