package main

import "github.com/spf13/cobra"

// authGuidance is the single source of truth for how to set up CLI auth.
// Referenced by auth_login.go, auth_status.go, and the auth group help below.
const authGuidance = `Three ways to provide your API key:
  1. Environment variable (current shell only):
       export HEYGEN_API_KEY=<your-key>
  2. Pipe to auth login (saves to ~/.heygen/credentials):
       echo "$KEY" | heygen auth login
  3. Interactive prompt (saves to ~/.heygen/credentials):
       heygen auth login

When both env var and stored credential are set, the env var takes priority.

Get a key: https://app.heygen.com/settings/api`

func newAuthCmd(ctx *cmdContext) *cobra.Command {
	cmd := newCommandGroup("auth", "Manage authentication")
	cmd.Long = "Manage how the CLI authenticates with the HeyGen API.\n\n" + authGuidance
	cmd.AddCommand(newAuthLoginCmd(ctx))
	cmd.AddCommand(newAuthStatusCmd(ctx))
	return cmd
}
