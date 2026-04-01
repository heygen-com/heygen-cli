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
	groups, err := GroupEndpoints(doc, examples)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// Step 4: Validate that all commands have examples
	if err := validateExamples(groups); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// Step 5: Generate Go source files
	if err := Generate(groups, "codegen/templates", *outDir); err != nil {
		fmt.Fprintf(os.Stderr, "error generating: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Generated %d command groups into %s\n", len(groups), *outDir)
}

// validateExamples ensures every command has at least one example.
func validateExamples(groups command.Groups) error {
	for _, specs := range groups {
		for _, spec := range specs {
			if len(spec.Examples) == 0 {
				return fmt.Errorf("missing examples for %s %s (add to codegen/examples/)", spec.Method, spec.Endpoint)
			}
		}
	}
	return nil
}
