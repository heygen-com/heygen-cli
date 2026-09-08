package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
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
			credMeta := credentialMetadata()
			if credMeta == nil {
				result, err := executeAuthStatusRequest(ctx, gen.UserMeGet)
				if err != nil {
					return err
				}
				return ctx.formatter.Data(result, client.APIDataField, nil)
			}

			isAPIKey := credMeta["type"] == "api_key"
			if isAPIKey {
				apiKeyResult, err := executeAuthStatusRequest(ctx, apiKeySelfSpec)
				if err != nil {
					reason, fatal := classifyAPIKeySelfFailure(err)
					if fatal {
						return err
					}
					// Recorded inline as well as on stderr: a JSON consumer, or
					// anyone piping stdout, has to be able to tell "we could not
					// read this key's permissions" apart from "this key has none".
					credMeta["permissions_unavailable"] = reason
					ctx.formatter.Warn("/v3/api_keys/self did not return permission metadata — " + reason)
				} else if err := mergeAPIKeyMetadata(apiKeyResult, credMeta); err != nil {
					return clierrors.New("failed to assemble API key status: " + err.Error())
				}
			}

			result, err := executeAuthStatusRequest(ctx, gen.UserMeGet)
			if err != nil {
				if !isAPIKey || !isInsufficientAPIKeyScope(err) {
					return err
				}
				result = json.RawMessage(`{"data":{}}`)
			}
			if !isAPIKey {
				// The OAuth transport may have refreshed and persisted the
				// credential while executing /v3/users/me. Re-resolve after
				// that request so the status reflects the new expiry and scope.
				if refreshedMeta := credentialMetadata(); refreshedMeta != nil {
					credMeta = refreshedMeta
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

func executeAuthStatusRequest(ctx *cmdContext, spec *command.Spec) (json.RawMessage, error) {
	return ctx.client.Execute(spec, &command.Invocation{
		PathParams:  make(map[string]string),
		QueryParams: make(url.Values),
	})
}

func isInsufficientAPIKeyScope(err error) bool {
	var cliErr *clierrors.CLIError
	return errors.As(err, &cliErr) &&
		cliErr.HTTPStatus == http.StatusForbidden &&
		cliErr.Code == "insufficient_api_key_scope"
}

// classifyAPIKeySelfFailure decides how a /v3/api_keys/self failure is reported,
// returning the reason to show for a degrade and whether the failure is fatal.
//
// `auth status` is a diagnostic: it is run precisely when something already
// looks wrong, so failing to *enrich* the credential with permission metadata
// must not take down the answer the CLI can always give — which credential is
// in use, and whether it authenticates. Only 401 is fatal, because "this
// credential was rejected" is the one question this command must never answer
// optimistically.
//
// Everything else degrades, but each class carries its own reason: the routine
// pre-deploy state and a genuine server-side anomaly must never render
// identically, or nobody goes looking when it is the anomaly.
func classifyAPIKeySelfFailure(err error) (reason string, fatal bool) {
	var cliErr *clierrors.CLIError
	if !errors.As(err, &cliErr) || cliErr.HTTPStatus == 0 {
		// Network failure, timeout, DNS — no HTTP response to classify.
		return "the request did not complete (" + err.Error() + "), which is usually transient", false
	}

	switch status := cliErr.HTTPStatus; {
	case status == http.StatusUnauthorized:
		return "", true
	case status == http.StatusNotFound:
		return "this server does not support /v3/api_keys/self yet", false
	case status == http.StatusForbidden:
		// /v3/api_keys/self deliberately carries no x-heygen-required-scopes:
		// any valid key may introspect itself. So a 403 here cannot be a
		// legitimately under-scoped key, and must not be presented as the
		// routine "not deployed yet" state.
		return "unexpected — /v3/api_keys/self requires no scope grant, so a 403 points at a server-side scope-exemption regression or an upstream proxy intercepting the request", false
	case status >= 500:
		return "the server reported HTTP " + strconv.Itoa(status) + ", which is usually transient", false
	default:
		return "the server reported HTTP " + strconv.Itoa(status), false
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
	locallyOwned := map[string]struct{}{
		"source": {},
		"type":   {},
		"user":   {},
	}
	for key, value := range envelope.Data {
		if _, owned := locallyOwned[key]; owned {
			continue
		}
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
