package main

import (
	"errors"
	"os"
	"strings"
	"time"

	"github.com/heygen-com/heygen-cli/internal/analytics"
	"github.com/heygen-com/heygen-cli/internal/config"
	clierrors "github.com/heygen-com/heygen-cli/internal/errors"
)

// version is set at build time via ldflags.
var version = "dev"

func main() {
	// Bootstrap formatter created before the Cobra tree so it's available
	// for errors returned from command execution, including in --human mode.
	formatter := formatterForArgs(os.Args[1:], os.Stdout, os.Stderr)
	analyticsClient := analytics.New(version, analyticsEnabled())

	cmd := newRootCmd(version, formatter, analyticsClient)
	start := time.Now()
	executedCmd, err := cmd.ExecuteC()

	exitCode := 0
	errorCode := ""
	if err != nil {
		var cliErr *clierrors.CLIError
		if errors.As(err, &cliErr) {
			formatter.Error(cliErr)
			exitCode = cliErr.ExitCode
			errorCode = cliErr.Code
		} else {
			// Cobra returns plain errors for unknown commands and arg validation.
			// Detect these and wrap as usage errors (exit 2).
			wrapped := classifyError(err)
			formatter.Error(wrapped)
			exitCode = wrapped.ExitCode
			errorCode = wrapped.Code
		}
	}

	if analyticsClient.Started() && executedCmd != nil {
		isAPICall := executedCmd != nil && !skipAuth(executedCmd) && !isSchemaRequest(executedCmd)
		analyticsClient.CommandRunComplete(executedCmd.CommandPath(), exitCode, time.Since(start), analytics.CommandRunCompleteOpts{
			ErrorCode: errorCode,
			APICall:   isAPICall,
		})
	}
	analyticsClient.Close()
	os.Exit(exitCode)
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

func analyticsEnabled() bool {
	if os.Getenv("HEYGEN_NO_ANALYTICS") != "" {
		return false
	}
	fp := &config.FileProvider{}
	val, found, err := fp.Get(config.KeyAnalytics)
	if err == nil && found && val == "false" {
		return false
	}
	return true
}
