package main

import "github.com/spf13/cobra"

// authGuidance is the single source of truth for how to set up CLI auth.
// Referenced by auth_login.go, auth_status.go, and the auth group help below.
const authGuidance = `Two ways to provide your API key:
  1. Environment variable:
       export HEYGEN_API_KEY=<your-key>
  2. Stored credential file:
       echo "$KEY" | heygen auth login    # piped
       heygen auth login                  # interactive prompt

When both are set, the env var takes priority.

Get a key: https://app.heygen.com/settings/api`

func newAuthCmd(ctx *cmdContext) *cobra.Command {
	cmd := newCommandGroup("auth", "Manage authentication")
	cmd.Long = "Manage how the CLI authenticates with the HeyGen API.\n\n" + authGuidance
	cmd.AddCommand(newAuthLoginCmd(ctx))
	cmd.AddCommand(newAuthStatusCmd(ctx))
	return cmd
}
