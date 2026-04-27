package main

import (
	"bytes"
	"context"
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
	"github.com/spf13/pflag"
	"golang.org/x/term"
)

// buildCobraCommand creates a Cobra command from a command.Spec.
// It registers typed flags, sets up positional arg validation, and wires
// RunE to build an Invocation and execute it through the HTTP client.
func buildCobraCommand(spec *command.Spec, ctx *cmdContext) *cobra.Command {
	var rawData string

	description := spec.Description
	if spec.BodyEncoding == "json" && !hasBodyFlags(spec) {
		description += "\n\nUse --request-schema to see all available request fields."
	}

	cmd := &cobra.Command{
		Use:     buildUseLine(spec),
		Short:   spec.Summary,
		Long:    description,
		Example: strings.Join(spec.Examples, "\n"),
		Args: func(cmd *cobra.Command, args []string) error {
			if isSchemaRequest(cmd) {
				return nil
			}
			return cobra.ExactArgs(len(spec.Args))(cmd, args)
		},
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if isSchemaRequest(cmd) {
				clearRequiredFlagAnnotations(cmd, nil)
			} else if cmd.Flags().Changed("data") {
				clearRequiredFlagAnnotations(cmd, bodyFlagNames(spec))
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			defer restoreRequiredFlagAnnotations(cmd)

			if show, schema := requestedSchema(cmd, spec); show {
				var buf bytes.Buffer
				if err := json.Compact(&buf, []byte(schema)); err != nil {
					// Fallback: print as-is if compact fails.
					_, err = fmt.Fprintln(cmd.OutOrStdout(), schema)
					return err
				}
				_, err := fmt.Fprintln(cmd.OutOrStdout(), buf.String())
				return err
			}

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

			// POST/PUT/PATCH commands with JSON bodies must have input
			// unless the request schema is intentionally empty (e.g.
			// `video-agent stop`). Without this check, an empty body is
			// sent to the API and returns a confusing 400.
			if spec.BodyEncoding == "json" && inv.Body == nil && spec.Method != "DELETE" && !requestBodyOptional(spec) {
				return clierrors.NewUsage(
					fmt.Sprintf("request body required: use -d '{...}' or pass individual flags. Run 'heygen %s %s --request-schema' to see the expected format", spec.Group, spec.Name))
			}

			if spec.Destructive {
				force, _ := cmd.Flags().GetBool("force")
				if !force {
					if !stdinIsTerminalFunc() {
						return &clierrors.CLIError{
							Code:     "confirmation_required",
							Message:  fmt.Sprintf("destructive operation %q requires confirmation", spec.Name),
							Hint:     "Use --force to skip confirmation in non-interactive environments",
							ExitCode: clierrors.ExitGeneral,
						}
					}
					if err := confirmAction(
						cmd.ErrOrStderr(),
						cmd.InOrStdin(),
						fmt.Sprintf("Warning: %s. Continue?", spec.Summary),
					); err != nil {
						return err
					}
				}
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
							notRetryable := false
							return &clierrors.CLIError{
								Code:      "timeout",
								Message:   fmt.Sprintf("still processing after --wait of %s (not failed, just not done yet)", timeout),
								Hint:      hint,
								ExitCode:  clierrors.ExitTimeout,
								Retryable: &notRetryable,
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

	if spec.RequestSchema != "" {
		cmd.Flags().Bool("request-schema", false, "Output the underlying API request body JSON schema and exit")
	}
	if spec.ResponseSchema != "" {
		cmd.Flags().Bool("response-schema", false, "Output the underlying API response JSON schema and exit")
	}
	if pollConfigs[spec.Group+"/"+spec.Name] != nil {
		cmd.Flags().Bool("wait", false, "Poll until the operation completes or fails")
		cmd.Flags().Duration("timeout", 20*time.Minute, "Max time to wait when using --wait")
	}
	// Add -d/--data for commands with JSON request bodies
	if spec.BodyEncoding == "json" {
		cmd.Flags().StringVarP(&rawData, "data", "d", "",
			"JSON request body (inline JSON, file path, or - for stdin). When used with individual flags, flags override matching fields in the JSON.")
	}
	if spec.Destructive {
		cmd.Flags().Bool("force", false, "Skip confirmation prompt for destructive operations")
	}

	return cmd
}

// commandPathParts splits a generated command path into nested Cobra tokens.
// Example: "proofreads srt update" -> ["proofreads", "srt", "update"].
func commandPathParts(spec *command.Spec) []string {
	return strings.Fields(spec.Name)
}

// buildUseLine constructs the leaf Cobra Use string from the spec name and args.
// Example: "create <video-id>" or "list".
func hasBodyFlags(spec *command.Spec) bool {
	for _, f := range spec.Flags {
		if f.Source == "body" {
			return true
		}
	}
	return false
}

// requestBodyOptional reports whether the command's request schema accepts
// an empty body. True when the schema has no properties, no required fields,
// and no discriminators (oneOf/anyOf/allOf). Used to let commands like
// `video-agent stop` — which POSTs an empty body by design — through the
// empty-body guard in RunE.
func requestBodyOptional(spec *command.Spec) bool {
	if spec.RequestSchema == "" {
		return true
	}
	var s struct {
		Properties map[string]any `json:"properties"`
		Required   []string       `json:"required"`
		OneOf      []any          `json:"oneOf"`
		AnyOf      []any          `json:"anyOf"`
		AllOf      []any          `json:"allOf"`
	}
	if err := json.Unmarshal([]byte(spec.RequestSchema), &s); err != nil {
		return false
	}
	return len(s.Properties) == 0 &&
		len(s.Required) == 0 &&
		len(s.OneOf) == 0 &&
		len(s.AnyOf) == 0 &&
		len(s.AllOf) == 0
}

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

func requestedSchema(cmd *cobra.Command, spec *command.Spec) (bool, string) {
	if spec.RequestSchema != "" {
		if show, _ := cmd.Flags().GetBool("request-schema"); show {
			return true, spec.RequestSchema
		}
	}
	if spec.ResponseSchema != "" {
		if show, _ := cmd.Flags().GetBool("response-schema"); show {
			return true, spec.ResponseSchema
		}
	}
	return false, ""
}

type requiredFlagAnnotationsKey struct{}

func clearRequiredFlagAnnotations(cmd *cobra.Command, filter map[string]bool) {
	existing, _ := cmd.Context().Value(requiredFlagAnnotationsKey{}).(map[string][]string)
	saved := make(map[string][]string, len(existing))
	for name, annotations := range existing {
		saved[name] = append([]string(nil), annotations...)
	}
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		if filter != nil && !filter[f.Name] {
			return
		}
		annotations, ok := f.Annotations[cobra.BashCompOneRequiredFlag]
		if !ok {
			return
		}
		if _, alreadySaved := saved[f.Name]; !alreadySaved {
			saved[f.Name] = append([]string(nil), annotations...)
		}
		delete(f.Annotations, cobra.BashCompOneRequiredFlag)
		if len(f.Annotations) == 0 {
			f.Annotations = nil
		}
	})
	if len(saved) == 0 {
		return
	}
	cmd.SetContext(context.WithValue(cmd.Context(), requiredFlagAnnotationsKey{}, saved))
}

func bodyFlagNames(spec *command.Spec) map[string]bool {
	names := make(map[string]bool)
	for _, flag := range spec.Flags {
		if flag.Source == "body" {
			names[flag.Name] = true
		}
	}
	return names
}

func restoreRequiredFlagAnnotations(cmd *cobra.Command) {
	saved, _ := cmd.Context().Value(requiredFlagAnnotationsKey{}).(map[string][]string)
	if len(saved) == 0 {
		return
	}
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		annotations, ok := saved[f.Name]
		if !ok {
			return
		}
		if f.Annotations == nil {
			f.Annotations = make(map[string][]string)
		}
		f.Annotations[cobra.BashCompOneRequiredFlag] = append([]string(nil), annotations...)
	})
}

// registerFlag adds a typed flag to the Cobra command based on the FlagSpec.
func registerFlag(cmd *cobra.Command, flag command.FlagSpec) {
	helpText := flag.Help
	if flag.Required {
		helpText += " (required)"
	}
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
