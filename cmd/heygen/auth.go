package main

import "github.com/spf13/cobra"

// Shared auth constants — single source of truth for auth guidance strings.
// Referenced by auth_login.go, auth_status.go, and the auth group help below.
const (
	authKeyURL = "https://app.heygen.com/settings/api"
	authEnvVar = "HEYGEN_API_KEY"

	// authFixHint is the standard actionable hint for auth failures.
	authFixHint = "Set:  export " + authEnvVar + "=<your-key>\n" +
		"Or:   echo \"$KEY\" | heygen auth login\n" +
		"Get a key: " + authKeyURL
)

func newAuthCmd(ctx *cmdContext) *cobra.Command {
	cmd := newCommandGroup("auth", "Manage authentication")
	cmd.Long = `Manage how the CLI authenticates with the HeyGen API.

Two ways to provide your API key:
  1. Environment variable:
       export ` + authEnvVar + `=<your-key>
  2. Stored credential file:
       echo "$KEY" | heygen auth login    # piped
       heygen auth login                  # interactive prompt

When both are set, the env var takes priority.

Get a key: ` + authKeyURL
	cmd.AddCommand(newAuthLoginCmd(ctx))
	cmd.AddCommand(newAuthStatusCmd(ctx))
	return cmd
}
