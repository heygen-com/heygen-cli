package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
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
					t.Errorf("%s job %q pins go-version: %q; use go-version-file: go.mod so the version has one home", e.Name(), job, v)
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

// The workflows above read go.mod, so go.mod is now what decides which
// toolchain CI and the release builds install. That makes two properties of the
// file load-bearing, and neither is visible from reading a workflow.
//
// First, setup-go matches its input with semver.satisfies, so the number of
// components decides pinned versus floating: `go 1.25.13` admits only that
// patch, while `go 1.26` admits any 1.26.x. The repo's choice is a reproducible
// pin, so the directive must carry a patch component.
//
// Second, setup-go prefers a `toolchain` directive over the `go` directive
// unless GOTOOLCHAIN is already "local". It is not: setup-go resolves the
// version before it exports that variable, so a toolchain line silently wins
// and go.mod stops being a single source while still appearing to be one. Go
// tooling can add that line on its own, which is why this is a test and not a
// convention. Bump the `go` directive instead.
//
// Requiring three components also rejects an exact prerelease pin like
// `go 1.26rc1`. That is intended: releases ship binaries to users, so a
// prerelease toolchain is not something to build them with.
//
// Out of scope: a workflow that sets GOTOOLCHAIN itself would override
// selection after setup-go runs. Nothing does, and unlike the two cases above
// that would be a deliberate act rather than drift.
func TestGoModPinsAnExactToolchain(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}

	if m := regexp.MustCompile(`(?m)^toolchain\s+(\S+)`).FindSubmatch(raw); m != nil {
		t.Errorf("go.mod has a toolchain directive (%s); setup-go would install that instead of the go directive; remove it and bump `go` instead", m[1])
	}

	m := regexp.MustCompile(`(?m)^go\s+(\S+)`).FindSubmatch(raw)
	if m == nil {
		t.Fatal("go.mod has no go directive; every setup-go step resolves its version from it")
	}
	version := string(m[1])

	if parts := strings.Split(version, "."); len(parts) != 3 {
		t.Errorf("go directive is %q, want major.minor.patch. setup-go treats %q as a range and would install the latest matching patch", version, version)
	}
}
