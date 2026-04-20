// Package main implements the codegen pipeline that reads an OpenAPI spec
// and produces command.Spec Go files for the HeyGen CLI.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/heygen-com/heygen-cli/internal/command"
)

func main() {
	specPath := flag.String("spec", "", "Path to OpenAPI JSON spec")
	outDir := flag.String("out", "gen/", "Output directory for generated files")
	examplesPath := flag.String("examples", "", "Path to examples YAML file or directory")
	strict := flag.Bool("strict", false, "Fail on missing examples (use in CI)")
	flag.Parse()

	if *specPath == "" {
		fmt.Fprintln(os.Stderr, "error: -spec flag is required")
		os.Exit(1)
	}

	// Step 1: Load examples
	var examples Examples
	if *examplesPath != "" {
		var err error
		examples, err = LoadExamples(*examplesPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error loading examples: %v\n", err)
			os.Exit(1)
		}
	} else {
		examples = make(Examples)
	}

	// Step 2: Load the OpenAPI spec
	doc, err := openapi3.NewLoader().LoadFromFile(*specPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading spec: %v\n", err)
		os.Exit(1)
	}

	// Step 3: Group endpoints into command specs
	groups, descriptions, err := GroupEndpoints(doc, examples)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// Step 4: Validate that all commands have examples
	warnings := validateExamples(groups)
	if len(warnings) > 0 {
		for _, w := range warnings {
			fmt.Fprintf(os.Stderr, "warning: %s\n", w)
		}
		if *strict {
			fmt.Fprintf(os.Stderr, "error: %d command(s) missing examples (strict mode)\n", len(warnings))
			os.Exit(1)
		}
	}

	// Step 5: Generate Go source files
	if err := Generate(groups, descriptions, "codegen/templates", *outDir); err != nil {
		fmt.Fprintf(os.Stderr, "error generating: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Generated %d command groups into %s\n", len(groups), *outDir)
	if len(warnings) > 0 {
		fmt.Fprintf(os.Stderr, "%d command(s) missing examples — add to codegen/examples/\n", len(warnings))
	}
}

// validateExamples returns a warning for each command missing examples.
func validateExamples(groups command.Groups) []string {
	var warnings []string
	for _, specs := range groups {
		for _, spec := range specs {
			if len(spec.Examples) == 0 {
				warnings = append(warnings, fmt.Sprintf("missing examples for %s %s (add to codegen/examples/)", spec.Method, spec.Endpoint))
			}
		}
	}
	return warnings
}
