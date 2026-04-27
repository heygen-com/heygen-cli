package main

import (
	"github.com/heygen-com/heygen-cli/internal/auth"
	clierrors "github.com/heygen-com/heygen-cli/internal/errors"
)

// enrichAuthHint sets a source-aware hint on auth errors that don't already
// have one. Called once in the centralized error path (main.go / testutil),
// not scattered across individual commands.
func enrichAuthHint(cliErr *clierrors.CLIError, source auth.CredentialSource) {
	if cliErr.ExitCode != clierrors.ExitAuth {
		return
	}
	if cliErr.Hint != "" {
		return
	}
	switch source {
	case auth.SourceEnv:
		cliErr.Hint = "The HEYGEN_API_KEY environment variable contains an invalid or expired key.\nGenerate a new key: https://app.heygen.com/settings/api"
	case auth.SourceFile:
		cliErr.Hint = "The stored API key (~/.heygen/credentials) is invalid or expired.\nReplace it: heygen auth login\nGenerate a new key: https://app.heygen.com/settings/api"
	}
}
