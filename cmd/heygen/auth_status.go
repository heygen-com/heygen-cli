package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/heygen-com/heygen-cli/gen"
	"github.com/heygen-com/heygen-cli/internal/auth"
	"github.com/heygen-com/heygen-cli/internal/client"
	"github.com/heygen-com/heygen-cli/internal/command"
	clierrors "github.com/heygen-com/heygen-cli/internal/errors"
	"github.com/spf13/cobra"
)

var apiKeySelfSpec = &command.Spec{
	Endpoint: "/v3/api_keys/self",
	Method:   http.MethodGet,
}

func newAuthStatusCmd(ctx *cmdContext) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Verify the active credential (API key or OAuth) and show account info",
		Long: "Verifies the credential currently in use by calling the HeyGen API.\n\n" +
			"For API keys, reports the key name, permission mode and scopes,\n" +
			"creation and update times, and expiration. For OAuth credentials,\n" +
			"reports the credential source, expiry, scope, and refreshability.\n\n" + authGuidance,
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
			credMeta := credentialMetadata()
			if credMeta == nil {
				return ctx.formatter.Data(result, client.APIDataField, nil)
			}
			if credMeta["type"] == "api_key" {
				apiKeyResult, err := ctx.client.Execute(apiKeySelfSpec, &command.Invocation{
					PathParams:  make(map[string]string),
					QueryParams: make(url.Values),
				})
				if err != nil {
					return err
				}
				if err := mergeAPIKeyMetadata(apiKeyResult, credMeta); err != nil {
					return clierrors.New("failed to assemble API key status: " + err.Error())
				}
			}
			merged, err := mergeStatusEnvelope(result, credMeta)
			if err != nil {
				return clierrors.New("failed to assemble auth status: " + err.Error())
			}
			return ctx.formatter.Data(merged, "", nil)
		},
	}
}

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
	// Friendly-display block — only present when the source is the
	// credentials file AND a user block was persisted at login time.
	// Env-based credentials (HEYGEN_API_KEY) deliberately don't carry
	// friendly fields because we never probe /v3/users/me on cold env
	// reads — the user can re-run `auth login` if they want them.
	if cred.Source == auth.SourceFile {
		if ui, loadErr := auth.LoadUserInfo(); loadErr == nil && !ui.IsZero() {
			userMeta := map[string]any{}
			if ui.Email != "" {
				userMeta["email"] = ui.Email
			}
			if ui.FirstName != "" {
				userMeta["first_name"] = ui.FirstName
			}
			if ui.LastName != "" {
				userMeta["last_name"] = ui.LastName
			}
			if ui.Username != "" {
				userMeta["username"] = ui.Username
			}
			if display := ui.DisplayName(); display != "" {
				userMeta["display_name"] = display
			}
			meta["user"] = userMeta
		}
	}
	return meta
}

func mergeAPIKeyMetadata(raw json.RawMessage, credMeta map[string]any) error {
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return errors.New("upstream response was not JSON")
	}
	if envelope.Data == nil {
		return errors.New("upstream response did not contain API key metadata")
	}
	for key, value := range envelope.Data {
		credMeta[key] = value
	}
	return nil
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
