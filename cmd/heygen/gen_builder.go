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

// buildGenCommand creates a Cobra command from a generated command.Spec.
// It registers flags, sets up positional arg validation, and wires the
// RunE to build an Invocation and execute it.
func buildGenCommand(spec *command.Spec, ctx *cmdContext) *cobra.Command {
	var dataFlag string

	cmd := &cobra.Command{
		Use:     buildUseLine(spec),
		Short:   spec.Summary,
		Long:    spec.Description,
		Example: strings.Join(spec.Examples, "\n"),
		Args:    cobra.ExactArgs(len(spec.Args)),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Parse -d/--data if provided
			var data map[string]any
			if dataFlag != "" {
				parsed, err := readData(dataFlag)
				if err != nil {
					return err
				}
				data = parsed
			}

			inv, err := spec.BuildInvocation(cmd, args, data)
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
	for _, f := range spec.Flags {
		registerFlag(cmd, f)
	}

	// Add -d/--data for commands with JSON request bodies
	if spec.BodyEncoding == "json" {
		cmd.Flags().StringVarP(&dataFlag, "data", "d", "",
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
func registerFlag(cmd *cobra.Command, f command.FlagSpec) {
	help := f.Help
	if len(f.Enum) > 0 {
		help += fmt.Sprintf(" (allowed: %s)", strings.Join(f.Enum, ", "))
	}

	switch f.Type {
	case "int":
		def := 0
		if f.Default != "" {
			def, _ = strconv.Atoi(f.Default)
		}
		cmd.Flags().Int(f.Name, def, help)
	case "bool":
		def := false
		if f.Default == "true" {
			def = true
		}
		cmd.Flags().Bool(f.Name, def, help)
	case "float64":
		def := 0.0
		if f.Default != "" {
			def, _ = strconv.ParseFloat(f.Default, 64)
		}
		cmd.Flags().Float64(f.Name, def, help)
	case "string-slice":
		cmd.Flags().StringSlice(f.Name, nil, help)
	default: // string
		cmd.Flags().String(f.Name, f.Default, help)
	}

	if f.Required {
		_ = cmd.MarkFlagRequired(f.Name)
	}
}

// readData reads JSON from inline string, file path, or stdin ("-").
func readData(input string) (map[string]any, error) {
	var data []byte
	var err error

	if input == "-" {
		data, err = io.ReadAll(os.Stdin)
		if err != nil {
			return nil, clierrors.New(fmt.Sprintf("failed to read stdin: %v", err))
		}
	} else if strings.HasPrefix(input, "{") || strings.HasPrefix(input, "[") {
		// Looks like inline JSON
		data = []byte(input)
	} else {
		// Treat as file path
		data, err = os.ReadFile(input)
		if err != nil {
			return nil, clierrors.New(fmt.Sprintf("failed to read file %q: %v", input, err))
		}
	}

	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, clierrors.NewUsage(fmt.Sprintf("invalid JSON in --data: %v", err))
	}
	return result, nil
}
