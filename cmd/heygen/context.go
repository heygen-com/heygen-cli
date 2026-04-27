package main

import (
	"context"
	"errors"
	"net/url"
	"os"

	"github.com/heygen-com/heygen-cli/internal/auth"
	"github.com/heygen-com/heygen-cli/internal/client"
	"github.com/heygen-com/heygen-cli/internal/config"
	clierrors "github.com/heygen-com/heygen-cli/internal/errors"
	"github.com/heygen-com/heygen-cli/internal/output"
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
// context during initContext. Returns SourceUnknown if not set (e.g. skipAuth
// commands or when credential resolution failed before storing).
func credSourceFromCmd(cmd *cobra.Command) auth.CredentialSource {
	if cmd == nil {
		return auth.SourceUnknown
	}
	src, _ := cmd.Context().Value(credSourceKey{}).(auth.CredentialSource)
	return src
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
	result, err := resolver.ResolveWithSource()
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
	cmd.SetContext(context.WithValue(cmd.Context(), credSourceKey{}, result.Source))

	baseURL := provider.BaseURL()
	if u, err := url.Parse(baseURL); err == nil && u.Scheme == "http" && os.Getenv("HEYGEN_ALLOW_HTTP") == "" {
		return clierrors.NewUsage("HEYGEN_API_BASE uses HTTP which transmits API keys in plaintext. Set HEYGEN_ALLOW_HTTP=1 to allow.")
	}

	ctx.client = client.New(result.Key,
		client.WithBaseURL(baseURL),
		client.WithUserAgent("heygen-cli/"+version),
	)

	human, _ := cmd.Flags().GetBool("human")
	if human {
		ctx.formatter = output.NewHumanFormatter(cmd.OutOrStdout(), cmd.ErrOrStderr())
	}

	return nil
}
