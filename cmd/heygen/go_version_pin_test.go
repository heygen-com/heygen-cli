package main

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"gopkg.in/yaml.v3"
)

// The Go version has exactly one home: the `go` directive in go.mod. Every
// setup-go step reads it via go-version-file rather than restating it.
//
// This is not just DRY. setup-go exports GOTOOLCHAIN=local, so the toolchain it
// installs cannot upgrade itself to satisfy go.mod: a literal pin that drifts
// BELOW the directive fails the build outright, and one that drifts above it
// builds against a newer stdlib than the module declares. Before this was
// centralized the same version string was repeated in seven places, held in
// agreement only by whoever remembered to grep.
//
// A literal go-version is what this guards against, so the check is on the
// workflow files rather than on go.mod.
//
// Scope: direct actions/setup-go steps in top-level workflow files. A reusable
// workflow or composite action could install Go without a step this sees. None
// exists today; if one is added, this needs to follow those references.
func TestWorkflowsTakeGoVersionFromGoMod(t *testing.T) {
	dir := filepath.Join("..", "..", ".github", "workflows")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read workflows dir: %v", err)
	}

	// Assert the sweep found something: an empty match set would pass silently
	// and look identical to a clean bill.
	steps := 0

	for _, e := range entries {
		// Both extensions: this repo already spells one config .goreleaser.yaml,
		// so a .yaml workflow is a plausible way to land outside this sweep.
		if ext := filepath.Ext(e.Name()); e.IsDir() || (ext != ".yml" && ext != ".yaml") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}

		var wf struct {
			Jobs map[string]struct {
				Steps []struct {
					Uses string            `yaml:"uses"`
					With map[string]string `yaml:"with"`
				} `yaml:"steps"`
			} `yaml:"jobs"`
		}
		if err := yaml.Unmarshal(raw, &wf); err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}

		for job, j := range wf.Jobs {
			for _, st := range j.Steps {
				if !regexp.MustCompile(`actions/setup-go@`).MatchString(st.Uses) {
					continue
				}
				steps++
				if v, ok := st.With["go-version"]; ok {
					t.Errorf("%s job %q pins go-version: %q — use go-version-file: go.mod so the version has one home", e.Name(), job, v)
				}
				if got := st.With["go-version-file"]; got != "go.mod" {
					t.Errorf("%s job %q has go-version-file: %q, want %q", e.Name(), job, got, "go.mod")
				}
			}
		}
	}

	if steps == 0 {
		t.Fatal("found no actions/setup-go steps; the check read nothing and would pass regardless of the pins")
	}
}
