package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/heygen-com/heygen-cli/internal/analytics"
	"github.com/heygen-com/heygen-cli/internal/config"
	clierrors "github.com/heygen-com/heygen-cli/internal/errors"
	"github.com/heygen-com/heygen-cli/internal/paths"
)

// version is set at build time via ldflags.
var version = "dev"

func main() {
	// Bootstrap formatter created before the Cobra tree so it's available
	// for errors returned from command execution, including in --human mode.
	formatter := formatterForArgs(os.Args[1:], os.Stdout, os.Stderr)
	enabled := analyticsEnabled()
	maybeShowTelemetryNotice(enabled, os.Stderr)
	analyticsClient := analytics.New(version, enabled)
	loginAnalytics = analyticsClient

	cmd := newRootCmd(version, formatter, analyticsClient)
	start := time.Now()
	executedCmd, err := cmd.ExecuteC()

	exitCode := 0
	errorCode := ""
	source := ""
	httpStatus := 0
	if err != nil {
		var cliErr *clierrors.CLIError
		if errors.As(err, &cliErr) {
			enrichAuthHint(cliErr, credSourceFromCmd(executedCmd))
			formatter.Error(cliErr)
			exitCode = cliErr.ExitCode
			errorCode = cliErr.Code
			source = cliErr.Source
			if source == "" {
				source = "cli" // any error the CLI raised without an explicit origin
			}
			httpStatus = cliErr.HTTPStatus
		} else {
			// Cobra returns plain errors for unknown commands and arg validation.
			// Detect these and wrap as usage errors (exit 2).
			wrapped := classifyError(err)
			formatter.Error(wrapped)
			exitCode = wrapped.ExitCode
			errorCode = wrapped.Code
			source = wrapped.Source
		}
	}

	if analyticsClient.Started() && executedCmd != nil {
		analyticsClient.CommandRunComplete(executedCmd.CommandPath(), exitCode, time.Since(start), errorCode, source, httpStatus)
	}
	analyticsClient.Close()
	os.Exit(exitCode)
}

// classifyError wraps non-CLIError errors with the appropriate exit code.
// Cobra usage errors (unknown command, wrong arg count) get exit 2;
// everything else gets exit 1.
func classifyError(err error) *clierrors.CLIError {
	msg := err.Error()
	var wrapped *clierrors.CLIError
	if strings.HasPrefix(msg, "unknown command") ||
		strings.HasPrefix(msg, "unknown flag") ||
		strings.HasPrefix(msg, "unknown shorthand flag") ||
		strings.Contains(msg, "accepts ") ||
		strings.HasPrefix(msg, "required flag") ||
		strings.HasPrefix(msg, "invalid argument") {
		wrapped = clierrors.NewUsage(msg)
	} else {
		wrapped = clierrors.New(msg)
	}
	wrapped.Source = "cli" // Cobra-wrapped errors are always CLI-origin.
	return wrapped
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

// telemetryNoticePath is heygen-cli's own record of whether the telemetry
// disclosure notice has already been shown — heygen-cli's own config
// directory, not the shared ~/.hyperframes/config.json (that file's write
// surface is kept to the anonymousId field alone; see internal/analytics).
func telemetryNoticePath() string {
	return filepath.Join(paths.ConfigDir(), "telemetry_notice_shown")
}

// maybeShowTelemetryNotice prints a one-time disclosure that heygen-cli
// sends usage telemetry and that signing in links it to the account email
// or username, mirroring hyperframes-oss's existing notice. Gated the same
// way analyticsEnabled() already gates the analytics client itself:
// nothing to disclose when analytics is disabled, and shown at most once
// per install. stderr is injectable so tests can assert on it without
// redirecting the process's real os.Stderr.
func maybeShowTelemetryNotice(enabled bool, stderr io.Writer) {
	if !enabled {
		return
	}
	path := telemetryNoticePath()
	if _, err := os.Stat(path); err == nil {
		return
	}
	fmt.Fprintln(stderr,
		"heygen-cli sends anonymous usage telemetry (command, os/arch, outcome). "+
			"Signing in via `heygen auth login` links this usage to your account email or username. "+
			"Opt out with HEYGEN_NO_ANALYTICS=1.")
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	_ = os.WriteFile(path, []byte("1"), 0o600)
}
