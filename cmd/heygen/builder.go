package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/heygen-com/heygen-cli/internal/client"
	"github.com/heygen-com/heygen-cli/internal/command"
	clierrors "github.com/heygen-com/heygen-cli/internal/errors"
	"github.com/spf13/cobra"
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

			if spec.Paginated {
				allPages, _ := cmd.Flags().GetBool("all")
				cursorFlagName := cursorFlagForSpec(spec)
				cursorSet := cursorFlagName != "" && cmd.Flags().Changed(cursorFlagName)

				if allPages && cursorSet {
					return clierrors.NewUsage(fmt.Sprintf("--all and --%s are mutually exclusive", cursorFlagName))
				}

				if allPages {
					// --all returns a flat array (no envelope), so dataField is empty.
					// Columns are still passed so --human renders a curated table.
					cols := defaultColumnsForSpec(spec)
					result, err := ctx.client.ExecuteAll(spec, inv)
					if err != nil {
						var truncErr *client.ErrPaginationTruncated
						if errors.As(err, &truncErr) {
							// Output partial data, then signal truncation via CLIError.
							// The error goes through formatter.Error() — no raw stderr writes.
							if fmtErr := ctx.formatter.Data(truncErr.Data, "", cols); fmtErr != nil {
								return fmtErr
							}
							return clierrors.New(truncErr.Error())
						}
						return err
					}

					return ctx.formatter.Data(result, "", cols)
				}
			}

			result, err := ctx.client.Execute(spec, inv)
			if err != nil {
				return err
			}

			return ctx.formatter.Data(result, spec.DataField, defaultColumnsForSpec(spec))
		},
	}

	// Register flags from spec
	for _, flag := range spec.Flags {
		registerFlag(cmd, flag)
	}

	if spec.Paginated {
		cmd.Flags().Bool("all", false, "Fetch all pages (returns flat JSON array instead of API envelope)")
	}

	// Add -d/--data for commands with JSON request bodies
	if spec.BodyEncoding == "json" {
		cmd.Flags().StringVarP(&rawData, "data", "d", "",
			"JSON request body (inline JSON, file path, or - for stdin)")
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

func cursorFlagForSpec(spec *command.Spec) string {
	for _, flag := range spec.Flags {
		if flag.JSONName == spec.TokenParam {
			return flag.Name
		}
	}
	return ""
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
