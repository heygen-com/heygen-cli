package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadExamplesNormalizesStructuredEntries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "examples.yaml")
	content := `"POST /v3/videos":
  - desc: "Create and wait"
    cmd: "heygen video create --wait"
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write examples: %v", err)
	}

	examples, err := LoadExamples(path)
	if err != nil {
		t.Fatalf("LoadExamples: %v", err)
	}

	got := examples["POST /v3/videos"]
	want := []string{"# Create and wait\n  heygen video create --wait"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("examples = %#v, want %#v", got, want)
	}
}

func TestLoadExamplesRejectsMissingDesc(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "examples.yaml")
	content := `"POST /v3/videos":
  - cmd: "heygen video create --wait"
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write examples: %v", err)
	}

	_, err := LoadExamples(path)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "missing desc") {
		t.Fatalf("err = %v, want missing desc", err)
	}
}

func TestLoadExamplesRejectsMissingCmd(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "examples.yaml")
	content := `"POST /v3/videos":
  - desc: "Create and wait"
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write examples: %v", err)
	}

	_, err := LoadExamples(path)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "missing cmd") {
		t.Fatalf("err = %v, want missing cmd", err)
	}
}
