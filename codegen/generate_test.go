package main

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

var update = flag.Bool("update", false, "update golden files")

func loadTestSpec(t *testing.T, path string) *openapi3.T {
	t.Helper()
	doc, err := openapi3.NewLoader().LoadFromFile(path)
	if err != nil {
		t.Fatalf("failed to load spec %s: %v", path, err)
	}
	return doc
}

func TestGoldenFiles(t *testing.T) {
	doc := loadTestSpec(t, "testdata/mini_spec.json")

	overrides, err := LoadOverrides("testdata/test_overrides.yaml")
	if err != nil {
		t.Fatalf("LoadOverrides failed: %v", err)
	}

	groups := GroupEndpoints(doc, overrides)

	if len(groups) == 0 {
		t.Fatal("expected at least one command group")
	}

	// Render to a temp directory
	outDir := t.TempDir()
	if err := Render(groups, "templates", outDir); err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	goldenDir := "testdata/golden"

	if *update {
		if err := os.MkdirAll(goldenDir, 0755); err != nil {
			t.Fatalf("creating golden dir: %v", err)
		}
		entries, err := os.ReadDir(outDir)
		if err != nil {
			t.Fatalf("reading output dir: %v", err)
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			content, err := os.ReadFile(filepath.Join(outDir, e.Name()))
			if err != nil {
				t.Fatalf("reading %s: %v", e.Name(), err)
			}
			if err := os.WriteFile(filepath.Join(goldenDir, e.Name()), content, 0644); err != nil {
				t.Fatalf("writing golden %s: %v", e.Name(), err)
			}
		}
		t.Log("Golden files updated")
		return
	}

	// Compare output to golden files
	goldenEntries, err := os.ReadDir(goldenDir)
	if err != nil {
		t.Fatalf("reading golden dir: %v", err)
	}

	for _, e := range goldenEntries {
		if e.IsDir() {
			continue
		}
		t.Run(e.Name(), func(t *testing.T) {
			golden, err := os.ReadFile(filepath.Join(goldenDir, e.Name()))
			if err != nil {
				t.Fatalf("reading golden file: %v", err)
			}

			actual, err := os.ReadFile(filepath.Join(outDir, e.Name()))
			if err != nil {
				t.Fatalf("reading generated file %s: %v", e.Name(), err)
			}

			if string(golden) != string(actual) {
				t.Errorf("output mismatch for %s\nRun with -update to refresh golden files",
					e.Name())
			}
		})
	}
}

func TestValidateExamplesMissing(t *testing.T) {
	groups := []CommandGroup{
		{
			Name: "test",
			Commands: []GroupedCommand{
				{Method: "GET", Endpoint: "/v3/test"},
			},
		},
	}

	overrides := &Overrides{Examples: map[string][]string{}}

	err := validateExamples(groups, overrides)
	if err == nil {
		t.Error("expected error for missing examples")
	}
}
