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
		Long: "Verifies the API key currently in use by calling the HeyGen API.\n\n" + authGuidance,
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
					cliErr.Hint = "Your API key is missing or invalid.\n" + authGuidance
				}
				return err
			}
			return ctx.formatter.Data(result, client.APIDataField, nil)
		},
	}
}
