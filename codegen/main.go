// Package main implements the codegen pipeline that reads an OpenAPI spec
// and produces command.Spec Go files for the HeyGen CLI.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/getkin/kin-openapi/openapi3"
)

func main() {
	specPath := flag.String("spec", "", "Path to OpenAPI JSON spec")
	outDir := flag.String("out", "gen/", "Output directory for generated files")
	overridesPath := flag.String("overrides", "", "Path to overrides YAML file")
	flag.Parse()

	if *specPath == "" {
		fmt.Fprintln(os.Stderr, "error: -spec flag is required")
		os.Exit(1)
	}

	// Step 1: Load overrides
	var overrides *Overrides
	if *overridesPath != "" {
		var err error
		overrides, err = LoadOverrides(*overridesPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error loading overrides: %v\n", err)
			os.Exit(1)
		}
	} else {
		overrides = &Overrides{}
	}

	// Step 2: Load the OpenAPI spec
	doc, err := openapi3.NewLoader().LoadFromFile(*specPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading spec: %v\n", err)
		os.Exit(1)
	}

	// Step 3: Group endpoints into commands (filtering, classification, naming all happen here)
	groups := GroupEndpoints(doc, overrides)

	// Step 4: Validate that all commands have examples
	if err := validateExamples(groups, overrides); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// Step 5: Render generated Go files
	if err := Render(groups, "codegen/templates", *outDir); err != nil {
		fmt.Fprintf(os.Stderr, "error rendering: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Generated %d command groups into %s\n", len(groups), *outDir)
}

// validateExamples ensures every command has at least one example.
func validateExamples(groups []CommandGroup, overrides *Overrides) error {
	for _, g := range groups {
		for _, cmd := range g.Commands {
			key := cmd.Method + " " + cmd.Endpoint
			examples := overrides.Examples[key]
			if len(examples) == 0 {
				return fmt.Errorf("missing examples for %s (add to overrides.yaml)", key)
			}
		}
	}
	return nil
}
