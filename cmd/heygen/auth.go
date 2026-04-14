package main

import "github.com/spf13/cobra"

func newAuthCmd(ctx *cmdContext) *cobra.Command {
	cmd := newCommandGroup("auth", "Manage authentication")
	cmd.Long = `Manage how the CLI authenticates with the HeyGen API.

Two ways to provide your API key:
  1. Environment variable (preferred for CI/agents/ephemeral shells):
       export HEYGEN_API_KEY=<your-key>
  2. Stored credential file (preferred for human users):
       echo "$KEY" | heygen auth login    # piped
       heygen auth login                  # interactive prompt

The env var takes priority over the stored file when both are set.

Get a key: https://app.heygen.com/settings/api`
	cmd.AddCommand(newAuthLoginCmd(ctx))
	cmd.AddCommand(newAuthStatusCmd(ctx))
	return cmd
}
