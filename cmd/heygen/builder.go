package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

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

			result, err := ctx.client.Execute(spec, inv)
			if err != nil {
				return err
			}

			return ctx.formatter.Data(result)
		},
	}

	// Register flags from spec
	for _, flag := range spec.Flags {
		registerFlag(cmd, flag)
	}

	// Add -d/--data for commands with JSON request bodies
	if spec.BodyEncoding == "json" {
		cmd.Flags().StringVarP(&rawData, "data", "d", "",
			"JSON request body (inline JSON, file path, or - for stdin)")
	}

	return cmd
}

// buildUseLine constructs the Cobra Use string from the spec name and args.
// Example: "get <video-id>" or "list"
func buildUseLine(spec *command.Spec) string {
	parts := []string{spec.Name}
	for _, arg := range spec.Args {
		parts = append(parts, "<"+arg.Name+">")
	}
	return strings.Join(parts, " ")
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
