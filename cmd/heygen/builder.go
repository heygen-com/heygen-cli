package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/heygen-com/heygen-cli/internal/client"
	"github.com/heygen-com/heygen-cli/internal/command"
	clierrors "github.com/heygen-com/heygen-cli/internal/errors"
	"github.com/heygen-com/heygen-cli/internal/output"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// buildCobraCommand creates a Cobra command from a command.Spec.
// It registers typed flags, sets up positional arg validation, and wires
// RunE to build an Invocation and execute it through the HTTP client.
func buildCobraCommand(spec *command.Spec, ctx *cmdContext) *cobra.Command {
	var rawData string

	cmd := &cobra.Command{
		Use:     buildUseLine(spec),
		Short:   spec.Summary,
		Long:    spec.Description,
		Example: strings.Join(spec.Examples, "\n"),
		Args:    cobra.ExactArgs(len(spec.Args)),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Parse -d/--data if provided
			var parsedData map[string]any
			if rawData != "" {
				parsed, err := readData(rawData)
				if err != nil {
					return err
				}
				parsedData = parsed
			}

			inv, err := spec.BuildInvocation(cmd, args, parsedData)
			if err != nil {
				return err
			}

			// Poll config is looked up at call time, not attached to Spec.
			// Spec is immutable (generated); poll configs are hand-written in cmd/.
			pc := pollConfigs[spec.Group+"/"+spec.Name]
			if pc != nil {
				wait, _ := cmd.Flags().GetBool("wait")
				if wait {
					timeout, _ := cmd.Flags().GetDuration("timeout")

					// Let ExecuteAndPoll own the timeout via ensurePollContext.
					// Don't wrap cmd.Context() with WithTimeout here.
					pollSpec := *spec
					pollSpec.PollConfig = pc

					opts := client.PollOptions{
						Timeout:   timeout,
						BaseDelay: 2 * time.Second,
						MaxDelay:  30 * time.Second,
					}
					// Only emit progress in human mode. JSON mode keeps stderr
					// clean for machine consumption (structured errors only).
					var spinner *output.PollSpinner
					if _, ok := ctx.formatter.(*output.HumanFormatter); ok {
						if isTerminal(cmd.ErrOrStderr()) {
							spinner = output.StartPollSpinner(cmd.ErrOrStderr())
							opts.OnStatus = func(status string, elapsed time.Duration) {
								spinner.UpdateStatus(status, elapsed)
							}
						} else {
							var lastStatus string
							opts.OnStatus = func(status string, elapsed time.Duration) {
								if status == lastStatus {
									return
								}
								fmt.Fprintf(cmd.ErrOrStderr(), "Polling: status=%s (elapsed %s)\n", status, elapsed.Round(time.Second))
								lastStatus = status
							}
						}
					}

					result, err := ctx.client.ExecuteAndPoll(cmd.Context(), &pollSpec, inv, opts)
					if spinner != nil {
						spinner.Stop()
					}
					if err != nil {
						var timeoutErr *client.ErrPollTimeout
						if errors.As(err, &timeoutErr) {
							if timeoutErr.Data != nil {
								if fmtErr := ctx.formatter.Data(timeoutErr.Data, "data", nil); fmtErr != nil {
									return fmtErr
								}
							}
							hint := "Re-run the corresponding get command to check the current status manually"
							if pc.HintCommand != "" {
								hint = fmt.Sprintf("heygen %s %s", pc.HintCommand, timeoutErr.ResourceID)
							}
							return &clierrors.CLIError{
								Code:     "timeout",
								Message:  fmt.Sprintf("polling timed out after %s", timeout),
								Hint:     hint,
								ExitCode: clierrors.ExitTimeout,
							}
						}
						var failErr *client.ErrPollFailed
						if errors.As(err, &failErr) {
							// Output the failure response (contains error details),
							// then signal failure via CLIError for exit code 1.
							if fmtErr := ctx.formatter.Data(failErr.Data, "data", nil); fmtErr != nil {
								return fmtErr
							}
							return clierrors.New(failErr.Error())
						}
						return err
					}

					// --wait result is a single resource from the GET endpoint.
					// Columns are nil — HumanFormatter renders single objects as
					// key-value pairs, not tables. This matches `heygen video get`.
					return ctx.formatter.Data(result, "data", nil)
				}
			}
			result, err := ctx.client.Execute(spec, inv)
			if err != nil {
				return err
			}

			return ctx.formatter.Data(result, client.APIDataField, defaultColumnsForSpec(spec))
		},
	}

	// Register flags from spec
	for _, flag := range spec.Flags {
		registerFlag(cmd, flag)
	}

	if pollConfigs[spec.Group+"/"+spec.Name] != nil {
		cmd.Flags().Bool("wait", false, "Poll until the operation completes or fails")
		cmd.Flags().Duration("timeout", 10*time.Minute, "Max time to wait when using --wait")
	}
	// Add -d/--data for commands with JSON request bodies
	if spec.BodyEncoding == "json" {
		cmd.Flags().StringVarP(&rawData, "data", "d", "",
			"JSON request body (inline JSON, file path, or - for stdin). When used with individual flags, flags override matching fields in the JSON.")
	}

	return cmd
}

// commandPathParts splits a generated command path into nested Cobra tokens.
// Example: "sessions messages create" -> ["sessions", "messages", "create"].
func commandPathParts(spec *command.Spec) []string {
	return strings.Fields(spec.Name)
}

// buildUseLine constructs the leaf Cobra Use string from the spec name and args.
// Example: "create <video-id>" or "list".
func buildUseLine(spec *command.Spec) string {
	path := commandPathParts(spec)
	name := spec.Name
	if len(path) > 0 {
		name = path[len(path)-1]
	}

	parts := []string{name}
	for _, arg := range spec.Args {
		parts = append(parts, "<"+arg.Name+">")
	}
	return strings.Join(parts, " ")
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

// registerFlag adds a typed flag to the Cobra command based on the FlagSpec.
func registerFlag(cmd *cobra.Command, flag command.FlagSpec) {
	helpText := flag.Help
	if len(flag.Enum) > 0 {
		helpText += fmt.Sprintf(" (allowed: %s)", strings.Join(flag.Enum, ", "))
	}

	switch flag.Type {
	case "int":
		defaultVal := 0
		if flag.Default != "" {
			defaultVal, _ = strconv.Atoi(flag.Default)
		}
		cmd.Flags().Int(flag.Name, defaultVal, helpText)
	case "bool":
		defaultVal := flag.Default == "true"
		cmd.Flags().Bool(flag.Name, defaultVal, helpText)
	case "float64":
		defaultVal := 0.0
		if flag.Default != "" {
			defaultVal, _ = strconv.ParseFloat(flag.Default, 64)
		}
		cmd.Flags().Float64(flag.Name, defaultVal, helpText)
	case "string-slice":
		cmd.Flags().StringSlice(flag.Name, nil, helpText)
	default: // string
		cmd.Flags().String(flag.Name, flag.Default, helpText)
	}

	if flag.Required {
		_ = cmd.MarkFlagRequired(flag.Name)
	}
}

// readData reads JSON from inline string, file path, or stdin ("-").
func readData(source string) (map[string]any, error) {
	var raw []byte
	var err error

	if source == "-" {
		raw, err = io.ReadAll(os.Stdin)
		if err != nil {
			return nil, clierrors.New(fmt.Sprintf("failed to read stdin: %v", err))
		}
	} else if strings.HasPrefix(source, "{") || strings.HasPrefix(source, "[") {
		// Inline JSON
		raw = []byte(source)
	} else {
		// File path
		raw, err = os.ReadFile(source)
		if err != nil {
			return nil, clierrors.New(fmt.Sprintf("failed to read file %q: %v", source, err))
		}
	}

	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, clierrors.NewUsage(fmt.Sprintf("invalid JSON in --data: %v", err))
	}
	return body, nil
}
