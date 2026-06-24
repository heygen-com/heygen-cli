package main

import (
	"encoding/json"
	"errors"
	"net/url"
	"time"

	"github.com/heygen-com/heygen-cli/gen"
	"github.com/heygen-com/heygen-cli/internal/auth"
	"github.com/heygen-com/heygen-cli/internal/client"
	"github.com/heygen-com/heygen-cli/internal/command"
	clierrors "github.com/heygen-com/heygen-cli/internal/errors"
	"github.com/spf13/cobra"
)

func newAuthStatusCmd(ctx *cmdContext) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Verify the active credential (API key or OAuth) and show account info",
		Long: "Verifies the credential currently in use by calling the HeyGen API.\n\n" +
			"For OAuth credentials, also reports the credential type, source,\n" +
			"expiry, scope, and refreshability so you can tell whether a\n" +
			"refresh is imminent without inspecting the credentials file.\n\n" + authGuidance,
		Example: "heygen auth status",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := ctx.client.Execute(gen.UserMeGet, &command.Invocation{
				PathParams:  make(map[string]string),
				QueryParams: make(url.Values),
			})
			if err != nil {
				return err
			}
			// Build the auth-status envelope: API user info on the
			// existing `data` key, plus credential metadata on a new
			// `credential` key. Existing api-key consumers see strictly
			// additive output.
			//
			// The credential metadata is best-effort — if we can't
			// re-resolve here we return the API response shape as-is
			// so the api-key happy path is unchanged.
			credMeta := credentialMetadata()
			if credMeta == nil {
				return ctx.formatter.Data(result, client.APIDataField, nil)
			}
			merged, err := mergeStatusEnvelope(result, credMeta)
			if err != nil {
				return clierrors.New("failed to assemble auth status: " + err.Error())
			}
			return ctx.formatter.Data(merged, client.APIDataField, nil)
		},
	}
}

// credentialMetadata re-resolves the credential (same chain
// initContext used) to describe its type/source/expiry without storing
// any of the secret values themselves. Returns nil on any resolution
// failure so the existing happy path remains unchanged for api-key
// users.
func credentialMetadata() map[string]any {
	resolver := &auth.ChainCredentialResolver{
		Resolvers: []auth.CredentialResolver{
			&auth.EnvCredentialResolver{},
			&auth.FileCredentialResolver{},
		},
	}
	cred, err := resolver.ResolveTypedCredential()
	if err != nil {
		return nil
	}
	meta := map[string]any{
		"source": string(cred.Source),
	}
	switch cred.Type {
	case auth.CredentialTypeAPIKey:
		meta["type"] = "api_key"
	case auth.CredentialTypeOAuth:
		meta["type"] = "oauth"
		meta["refreshable"] = cred.HasRefreshToken()
		meta["scope"] = cred.Scope
		if !cred.ExpiresAt.IsZero() {
			meta["expires_at"] = cred.ExpiresAt.UTC().Format(time.RFC3339)
			meta["expires_in_seconds"] = int(time.Until(cred.ExpiresAt).Seconds())
		}
	case auth.CredentialTypeOAuthExpired:
		meta["type"] = "oauth"
		meta["expired"] = true
		meta["refreshable"] = cred.HasRefreshToken()
		meta["scope"] = cred.Scope
		if !cred.ExpiresAt.IsZero() {
			meta["expires_at"] = cred.ExpiresAt.UTC().Format(time.RFC3339)
		}
	}
	return meta
}

// mergeStatusEnvelope folds the credential metadata into the
// {"data": {...}} envelope returned by GET /v3/users/me, preserving the
// data field's existing shape and adding a `credential` field at the
// top level so existing JSON consumers don't break.
func mergeStatusEnvelope(raw json.RawMessage, credMeta map[string]any) (json.RawMessage, error) {
	var envelope map[string]any
	if err := json.Unmarshal(raw, &envelope); err != nil {
		// /v3/users/me returned non-JSON — surface what we have rather
		// than mask the actual response.
		return nil, errors.New("upstream response was not JSON")
	}
	// `null` (or any JSON literal that decodes to a nil map) succeeds
	// the Unmarshal but leaves envelope nil, so the assignment below
	// would panic. Initialize a fresh map so the credential block still
	// lands cleanly when the API returns a null envelope. (W3)
	if envelope == nil {
		envelope = map[string]any{}
	}
	envelope["credential"] = credMeta
	out, err := json.Marshal(envelope)
	if err != nil {
		return nil, err
	}
	return out, nil
}
