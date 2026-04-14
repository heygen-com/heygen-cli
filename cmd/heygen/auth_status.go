package main

import (
	"errors"
	"net/url"

	"github.com/heygen-com/heygen-cli/gen"
	"github.com/heygen-com/heygen-cli/internal/client"
	"github.com/heygen-com/heygen-cli/internal/command"
	clierrors "github.com/heygen-com/heygen-cli/internal/errors"
	"github.com/spf13/cobra"
)

func newAuthStatusCmd(ctx *cmdContext) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Verify the active API key (env var or stored) and show account info",
		Long: `Verifies the API key currently in use by calling the HeyGen API.

The CLI resolves the active key in this order:
  1. HEYGEN_API_KEY environment variable
  2. ~/.heygen/credentials (set via 'heygen auth login')

Use this command to confirm your auth setup is working before running other commands.`,
		Example: "heygen auth status",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := ctx.client.Execute(gen.UserMeGet, &command.Invocation{
				PathParams:  make(map[string]string),
				QueryParams: make(url.Values),
			})
			if err != nil {
				var cliErr *clierrors.CLIError
				if errors.As(err, &cliErr) && cliErr.ExitCode == clierrors.ExitAuth {
					cliErr.Hint = "Your API key is missing or invalid.\n" +
						"  Set:  export HEYGEN_API_KEY=<your-key>\n" +
						"  Or:   echo \"$KEY\" | heygen auth login\n" +
						"  Get a key: https://app.heygen.com/settings/api"
				}
				return err
			}
			return ctx.formatter.Data(result, client.APIDataField, nil)
		},
	}
}
