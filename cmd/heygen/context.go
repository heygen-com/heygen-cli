package main

import (
	"context"
	"errors"
	"fmt"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/heygen-com/heygen-cli/internal/auth"
	"github.com/heygen-com/heygen-cli/internal/auth/oauth"
	"github.com/heygen-com/heygen-cli/internal/client"
	"github.com/heygen-com/heygen-cli/internal/config"
	clierrors "github.com/heygen-com/heygen-cli/internal/errors"
	"github.com/heygen-com/heygen-cli/internal/output"
	"github.com/heygen-com/heygen-cli/internal/paths"
	"github.com/spf13/cobra"
)

// cmdContext holds shared dependencies created in PersistentPreRunE
// and consumed by child commands via closures.
type cmdContext struct {
	client         *client.Client
	formatter      output.Formatter
	configProvider config.Provider
	version        string
}

// skipAuth checks whether the command (or any parent) is annotated to
// bypass credential resolution. Used by auth and config commands.
func skipAuth(cmd *cobra.Command) bool {
	if isSchemaRequest(cmd) {
		return true
	}
	for c := cmd; c != nil; c = c.Parent() {
		if c.Annotations != nil && c.Annotations["skipAuth"] == "true" {
			return true
		}
	}
	return false
}

func isSchemaRequest(cmd *cobra.Command) bool {
	return schemaFlagEnabled(cmd, "request-schema") || schemaFlagEnabled(cmd, "response-schema")
}

func schemaFlagEnabled(cmd *cobra.Command, name string) bool {
	flag := cmd.Flags().Lookup(name)
	if flag == nil || !flag.Changed {
		return false
	}
	enabled, err := cmd.Flags().GetBool(name)
	return err == nil && enabled
}

type credSourceKey struct{}

// credSourceFromCmd retrieves the credential source stored on the command
// context during initContext. Returns "" if not set (e.g. skipAuth commands
// or when credential resolution failed before storing).
func credSourceFromCmd(cmd *cobra.Command) auth.CredentialSource {
	if cmd == nil {
		return ""
	}
	src, _ := cmd.Context().Value(credSourceKey{}).(auth.CredentialSource)
	return src
}

// enrichAuthHint sets a source-aware hint on auth errors that don't already
// have one. Called once in the centralized error path (main.go / testutil),
// not scattered across individual commands.
func enrichAuthHint(cliErr *clierrors.CLIError, source auth.CredentialSource) {
	if cliErr.ExitCode != clierrors.ExitAuth {
		return
	}
	// Only enrich genuine not-authenticated errors. 403 (forbidden / permission)
	// also exits 3, but re-authenticating won't help, so never stamp a "log in"
	// hint on it — it carries its own permission-oriented hint.
	switch cliErr.Code {
	case "auth_error", "unauthorized", "invalid_credentials":
	default:
		return
	}
	if cliErr.Hint != "" {
		return
	}
	credPath := filepath.Join(paths.ConfigDir(), "credentials")
	switch source {
	case auth.SourceEnv:
		cliErr.Hint = "The HEYGEN_API_KEY environment variable contains an invalid or expired key.\nGenerate a new key: " + clierrors.APIKeySettingsURL
	case auth.SourceFile:
		cliErr.Hint = fmt.Sprintf("The stored API key (%s) is invalid or expired.\nReplace it: heygen auth login\nGenerate a new key: %s", credPath, clierrors.APIKeySettingsURL)
	}
}

// initContext sets up the config provider and, for commands that require
// auth, resolves credentials and creates the HTTP client.
func initContext(cmd *cobra.Command, version string, ctx *cmdContext) error {
	provider := &config.LayeredProvider{
		Env:  &config.EnvProvider{},
		File: &config.FileProvider{},
	}
	ctx.configProvider = provider

	if skipAuth(cmd) {
		ctx.client = nil
		return nil
	}

	resolver := &auth.ChainCredentialResolver{
		Resolvers: []auth.CredentialResolver{
			&auth.EnvCredentialResolver{},
			&auth.FileCredentialResolver{},
		},
	}
	cred, err := resolver.ResolveTypedCredential()
	if err != nil {
		// Enrich the generic cold-start auth error ("no API key found")
		// with the full auth guidance. Don't overwrite specific hints
		// like "Check the credentials file at ..." (broken file case).
		var cliErr *clierrors.CLIError
		if errors.As(err, &cliErr) && cliErr.ExitCode == clierrors.ExitAuth && cliErr.Message == "no API key found" {
			cliErr.Hint = authGuidance
		}
		return err
	}
	cmd.SetContext(context.WithValue(cmd.Context(), credSourceKey{}, cred.Source))

	baseURL := provider.BaseURL()
	if u, err := url.Parse(baseURL); err == nil && u.Scheme == "http" && os.Getenv("HEYGEN_ALLOW_HTTP") == "" {
		return clierrors.NewUsage("HEYGEN_API_BASE uses HTTP which transmits API keys in plaintext. Set HEYGEN_ALLOW_HTTP=1 to allow.")
	}

	opts := []client.Option{
		client.WithBaseURL(baseURL),
		client.WithUserAgent("heygen-cli/" + version),
	}
	if cred.IsOAuth() {
		opts = append(opts, client.WithOAuthClient(oauth.NewClient()))
	}
	if hdrs, _ := cmd.Flags().GetStringArray("headers"); len(hdrs) > 0 {
		parsed, err := parseAndValidateHeaders(hdrs)
		if err != nil {
			return err
		}
		opts = append(opts, client.WithExtraHeaders(parsed))
	}
	ctx.client = client.NewWithCredential(*cred, opts...)

	human, _ := cmd.Flags().GetBool("human")
	if human {
		ctx.formatter = output.NewHumanFormatter(cmd.OutOrStdout(), cmd.ErrOrStderr())
	}

	return nil
}

// allowedHeaders is the set of header names that --headers accepts.
// Add new entries here as new attribution or routing headers are defined.
var allowedHeaders = map[string]bool{
	"x-heygen-client-source": true,
}

var headerValueRe = regexp.MustCompile(`^[a-zA-Z0-9_\-\.]+$`)

func parseAndValidateHeaders(raw []string) (map[string]string, error) {
	parsed := make(map[string]string, len(raw))
	for _, h := range raw {
		k, v, ok := strings.Cut(h, ":")
		if !ok {
			return nil, clierrors.NewUsage("invalid --headers format: " + h + " (expected Key:Value)")
		}
		key := strings.ToLower(strings.TrimSpace(k))
		val := strings.TrimSpace(v)
		if !allowedHeaders[key] {
			return nil, clierrors.NewUsage("header " + key + " is not in the allowlist")
		}
		if !headerValueRe.MatchString(val) {
			return nil, clierrors.NewUsage("header value must be alphanumeric, hyphens, underscores, and dots only")
		}
		parsed[textproto.CanonicalMIMEHeaderKey(key)] = val
	}
	return parsed, nil
}
