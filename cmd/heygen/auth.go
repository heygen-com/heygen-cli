package main

import (
	"github.com/spf13/cobra"

	clierrors "github.com/heygen-com/heygen-cli/internal/errors"
)

// authGuidance is the single source of truth for how to set up CLI auth.
// Referenced by auth_login.go, auth_status.go, and the auth group help below.
var authGuidance = `Two ways to authenticate:

  1. Browser OAuth (default — opens https://app.heygen.com):
       heygen auth login

  2. API key (interactive prompt or piped on stdin):
       heygen auth login --api-key
       echo "$KEY" | heygen auth login --api-key

The HEYGEN_API_KEY environment variable also takes priority over any
stored credential when both are set.

Manage your session:
  heygen auth status    # verify the active credential + show metadata
  heygen auth logout    # clear the stored OAuth session

Get a key: ` + clierrors.APIKeySettingsURL

func newAuthCmd(ctx *cmdContext) *cobra.Command {
	cmd := newCommandGroup("auth", "Manage authentication")
	cmd.Long = "Manage how the CLI authenticates with the HeyGen API.\n\n" + authGuidance
	cmd.AddCommand(newAuthLoginCmd(ctx))
	cmd.AddCommand(newAuthStatusCmd(ctx))
	cmd.AddCommand(newAuthLogoutCmd(ctx))
	return cmd
}
