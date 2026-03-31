package main

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "update golden files")

func TestGoldenFiles(t *testing.T) {
	// Parse the mini spec
	endpoints, err := Parse("testdata/mini_spec.json")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	// Load test overrides
	overrides, err := LoadOverrides("testdata/test_overrides.yaml")
	if err != nil {
		t.Fatalf("LoadOverrides failed: %v", err)
	}

	// Group endpoints
	groups := GroupEndpoints(endpoints, overrides)

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
		// Update golden files
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
				t.Errorf("output mismatch for %s\n\nExpected:\n%s\n\nGot:\n%s\n\nRun with -update to refresh golden files",
					e.Name(), string(golden), string(actual))
			}
		})
	}

	// Also verify that all generated files have corresponding golden files
	outEntries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatalf("reading output dir: %v", err)
	}

	goldenSet := make(map[string]bool)
	for _, e := range goldenEntries {
		goldenSet[e.Name()] = true
	}
	for _, e := range outEntries {
		if !e.IsDir() && !goldenSet[e.Name()] {
			t.Errorf("generated file %s has no corresponding golden file", e.Name())
		}
	}
}

func TestSkipV1Endpoints(t *testing.T) {
	endpoints, err := Parse("testdata/mini_spec.json")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	overrides, err := LoadOverrides("testdata/test_overrides.yaml")
	if err != nil {
		t.Fatalf("LoadOverrides failed: %v", err)
	}

	groups := GroupEndpoints(endpoints, overrides)

	// Verify no "legacy" group (v1 endpoint should be skipped)
	for _, g := range groups {
		if g.Name == "legacy" {
			t.Error("v1 legacy endpoint should be skipped")
		}
	}
}

func TestPaginationDetection(t *testing.T) {
	endpoints, err := Parse("testdata/mini_spec.json")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	overrides, err := LoadOverrides("testdata/test_overrides.yaml")
	if err != nil {
		t.Fatalf("LoadOverrides failed: %v", err)
	}

	groups := GroupEndpoints(endpoints, overrides)

	// Find the widget list command
	var found bool
	for _, g := range groups {
		if g.Name != "widget" {
			continue
		}
		for _, cmd := range g.Commands {
			if cmd.Name == "list" {
				found = true
				if cmd.TokenField != "next_token" {
					t.Errorf("expected TokenField=next_token, got %q", cmd.TokenField)
				}
				if cmd.DataField != "data" {
					t.Errorf("expected DataField=data, got %q", cmd.DataField)
				}
			}
		}
	}
	if !found {
		t.Error("widget list command not found")
	}
}

func TestComplexFieldsSkipped(t *testing.T) {
	endpoints, err := Parse("testdata/mini_spec.json")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	overrides, err := LoadOverrides("testdata/test_overrides.yaml")
	if err != nil {
		t.Fatalf("LoadOverrides failed: %v", err)
	}

	groups := GroupEndpoints(endpoints, overrides)

	// Find the widget create command — "settings" (object) should be skipped
	for _, g := range groups {
		if g.Name != "widget" {
			continue
		}
		for _, cmd := range g.Commands {
			if cmd.Name == "create" {
				for _, f := range cmd.Flags {
					if f.JSONName == "settings" {
						t.Error("complex field 'settings' should be skipped from flags")
					}
				}
				return
			}
		}
	}
	t.Error("widget create command not found")
}

func TestMultipartDetection(t *testing.T) {
	endpoints, err := Parse("testdata/mini_spec.json")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	overrides, err := LoadOverrides("testdata/test_overrides.yaml")
	if err != nil {
		t.Fatalf("LoadOverrides failed: %v", err)
	}

	groups := GroupEndpoints(endpoints, overrides)

	// Find the upload command
	for _, g := range groups {
		if g.Name != "upload" {
			continue
		}
		for _, cmd := range g.Commands {
			if cmd.BodyEncoding != "multipart" {
				t.Errorf("expected BodyEncoding=multipart, got %q", cmd.BodyEncoding)
			}
			// Should have file arg from positional override
			if len(cmd.Args) != 1 || cmd.Args[0].Target != "file" {
				t.Errorf("expected file arg, got %+v", cmd.Args)
			}
			return
		}
	}
	t.Error("upload command not found")
}

func TestNestedAction(t *testing.T) {
	endpoints, err := Parse("testdata/mini_spec.json")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	overrides, err := LoadOverrides("testdata/test_overrides.yaml")
	if err != nil {
		t.Fatalf("LoadOverrides failed: %v", err)
	}

	groups := GroupEndpoints(endpoints, overrides)

	// Find widget activate command (nested action)
	for _, g := range groups {
		if g.Name != "widget" {
			continue
		}
		for _, cmd := range g.Commands {
			if cmd.Name == "activate" {
				if len(cmd.Args) != 1 {
					t.Errorf("expected 1 arg (widget-id), got %d", len(cmd.Args))
				}
				if cmd.Args[0].Target != "path" {
					t.Errorf("expected path target, got %q", cmd.Args[0].Target)
				}
				if cmd.BodyEncoding != "json" {
					t.Errorf("expected json body encoding, got %q", cmd.BodyEncoding)
				}
				return
			}
		}
	}
	t.Error("widget activate command not found")
}

func TestOverridesSkipPattern(t *testing.T) {
	o := &Overrides{
		Skip: []string{"* /v1/*", "* /v2/*", "GET /v3/secret"},
	}

	tests := []struct {
		method string
		path   string
		skip   bool
	}{
		{"GET", "/v1/legacy", true},
		{"POST", "/v2/videos", true},
		{"GET", "/v3/videos", false},
		{"GET", "/v3/secret", true},
		{"POST", "/v3/secret", false},
	}

	for _, tt := range tests {
		got := o.ShouldSkip(tt.method, tt.path)
		if got != tt.skip {
			t.Errorf("ShouldSkip(%s, %s) = %v, want %v", tt.method, tt.path, got, tt.skip)
		}
	}
}

func TestValidateExamplesMissing(t *testing.T) {
	groups := []CommandGroup{
		{
			Name: "test",
			Commands: []GroupedCommand{
				{
					Method:   "GET",
					Endpoint: "/v3/test",
				},
			},
		},
	}

	overrides := &Overrides{
		Examples: map[string][]string{}, // no examples
	}

	err := validateExamples(groups, overrides)
	if err == nil {
		t.Error("expected error for missing examples")
	}
}
