package main

import (
	"errors"
	"os"

	clierrors "github.com/heygen-com/heygen-cli/internal/errors"
	"github.com/heygen-com/heygen-cli/internal/output"
)

// version is set at build time via ldflags.
var version = "dev"

func main() {
	// Bootstrap formatter created before the Cobra tree so it's available
	// for errors from PersistentPreRunE (auth failures, config loading).
	formatter := output.DefaultJSONFormatter()

	cmd := newRootCmd(version, formatter)
	if err := cmd.Execute(); err != nil {
		var cliErr *clierrors.CLIError
		if errors.As(err, &cliErr) {
			formatter.Error(cliErr)
			os.Exit(cliErr.ExitCode)
		}
		// Wrap unexpected errors so they also get JSON envelope treatment.
		wrapped := clierrors.New(err.Error())
		formatter.Error(wrapped)
		os.Exit(wrapped.ExitCode)
	}
}
