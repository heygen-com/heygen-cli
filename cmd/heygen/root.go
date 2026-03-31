package main

import (
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
	client    *client.Client
	formatter output.Formatter
	configProvider config.Provider
}

func newRootCmd(version string, formatter output.Formatter) *cobra.Command {
	ctx := &cmdContext{formatter: formatter}

	root := &cobra.Command{
		Use:           "heygen",
		Short:         "HeyGen CLI — manage videos, avatars, and more",
		Version:       version,
		SilenceUsage:  true, // we handle usage errors ourselves
		SilenceErrors: true, // we handle error output ourselves
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// 1. Create config provider (BaseURL only — auth is Resolver's job)
			provider := &config.EnvProvider{}
			ctx.configProvider = provider

			// 2. Resolve credentials (env var today; file-based storage later)
			resolver := &auth.EnvCredentialResolver{}
			apiKey, err := resolver.Resolve()
			if err != nil {
				return err
			}

			// 3. Create client using config.Provider for BaseURL
			ctx.client = client.New(apiKey,
				client.WithBaseURL(provider.BaseURL()),
				client.WithUserAgent("heygen-cli/"+version),
			)

			return nil
		},
	}

	// Map Cobra flag-parsing errors to CLIError with ExitUsage (exit code 2).
	root.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return clierrors.NewUsage(err.Error())
	})

	// Register subcommands
	root.AddCommand(newVideoCmd(ctx))

	return root
}
