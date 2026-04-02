package main

import (
	"errors"
	"os"
	"strings"

	clierrors "github.com/heygen-com/heygen-cli/internal/errors"
)

// version is set at build time via ldflags.
var version = "dev"

func main() {
	// Bootstrap formatter created before the Cobra tree so it's available
	// for errors returned from command execution, including in --human mode.
	formatter := formatterForArgs(os.Args[1:], os.Stdout, os.Stderr)

	cmd := newRootCmd(version, formatter)
	if err := cmd.Execute(); err != nil {
		var cliErr *clierrors.CLIError
		if errors.As(err, &cliErr) {
			formatter.Error(cliErr)
			os.Exit(cliErr.ExitCode)
		}
		// Cobra returns plain errors for unknown commands and arg validation.
		// Detect these and wrap as usage errors (exit 2).
		wrapped := classifyError(err)
		formatter.Error(wrapped)
		os.Exit(wrapped.ExitCode)
	}
}

// classifyError wraps non-CLIError errors with the appropriate exit code.
// Cobra usage errors (unknown command, wrong arg count) get exit 2;
// everything else gets exit 1.
func classifyError(err error) *clierrors.CLIError {
	msg := err.Error()
	if strings.HasPrefix(msg, "unknown command") ||
		strings.HasPrefix(msg, "unknown flag") ||
		strings.HasPrefix(msg, "unknown shorthand flag") ||
		strings.Contains(msg, "accepts ") ||
		strings.HasPrefix(msg, "required flag") ||
		strings.HasPrefix(msg, "invalid argument") {
		return clierrors.NewUsage(msg)
	}
	return clierrors.New(msg)
}
