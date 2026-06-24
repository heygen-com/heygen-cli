package main

import (
	"github.com/spf13/cobra"

	clierrors "github.com/heygen-com/heygen-cli/internal/errors"
)

// authGuidance is the single source of truth for how to set up CLI auth.
// Referenced by auth_login.go, auth_status.go, and the auth group help below.
var authGuidance = `Two ways to authenticate:

  1. API key (recommended — uses API credits, interactive prompt or
     piped on stdin):
       heygen auth login --api-key
       echo "$KEY" | heygen auth login --api-key

  2. Browser OAuth (uses subscription credits — opens https://app.heygen.com):
       heygen auth login --oauth

In an interactive shell, plain "heygen auth login" presents a picker
defaulted to the API-key path. Non-interactive shells (piped stdin,
CI=true, HEYGEN_NONINTERACTIVE=1) skip the picker and run the API-key
flow so agents and scripts keep working unchanged.

The HEYGEN_API_KEY environment variable also takes priority over any
stored credential when both are set. The credentials file holds at
most one of api_key / oauth per session — running ` + "`heygen auth login`" + `
clears the other block on success.

Manage your session:
  heygen auth status    # verify the active credential + show metadata
  heygen auth logout    # clear the stored credential (api_key or OAuth)

Get a key: ` + clierrors.APIKeySettingsURL

func newAuthCmd(ctx *cmdContext) *cobra.Command {
	cmd := newCommandGroup("auth", "Manage authentication")
	cmd.Long = "Manage how the CLI authenticates with the HeyGen API.\n\n" + authGuidance
	cmd.AddCommand(newAuthLoginCmd(ctx))
	cmd.AddCommand(newAuthStatusCmd(ctx))
	cmd.AddCommand(newAuthLogoutCmd(ctx))
	return cmd
}
