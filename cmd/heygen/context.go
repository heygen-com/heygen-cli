package main

import (
	"github.com/heygen-com/heygen-cli/internal/auth"
	"github.com/heygen-com/heygen-cli/internal/client"
	"github.com/heygen-com/heygen-cli/internal/config"
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
	apiKey, err := resolver.Resolve()
	if err != nil {
		return err
	}

	ctx.client = client.New(apiKey,
		client.WithBaseURL(provider.BaseURL()),
		client.WithUserAgent("heygen-cli/"+version),
	)

	human, _ := cmd.Flags().GetBool("human")
	if human {
		ctx.formatter = output.NewHumanFormatter(cmd.OutOrStdout(), cmd.ErrOrStderr())
	}

	return nil
}
