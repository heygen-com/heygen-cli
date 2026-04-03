package main

import (
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

func loadGroupTestSpec(t *testing.T) *openapi3.T {
	t.Helper()
	doc, err := openapi3.NewLoader().LoadFromFile("testdata/test_spec.yaml")
	if err != nil {
		t.Fatalf("loading test spec: %v", err)
	}
	return doc
}

func loadTestExamples(t *testing.T) Examples {
	t.Helper()
	examples, err := LoadExamples("testdata/test_examples.yaml")
	if err != nil {
		t.Fatalf("loading test examples: %v", err)
	}
	return examples
}

func TestGroupEndpoints_FilterHidden(t *testing.T) {
	doc := loadGroupTestSpec(t)
	examples := loadTestExamples(t)
	groups, _, err := GroupEndpoints(doc, examples)
	if err != nil {
		t.Fatalf("GroupEndpoints: %v", err)
	}
	for name := range groups {
		if name == "legacy" || name == "hidden" {
			t.Errorf("x-cli-visible=false group %q should be filtered out", name)
		}
	}
}

func TestGroupEndpoints_GroupNames(t *testing.T) {
	doc := loadGroupTestSpec(t)
	examples := loadTestExamples(t)
	groups, _, err := GroupEndpoints(doc, examples)
	if err != nil {
		t.Fatalf("GroupEndpoints: %v", err)
	}
	// "Videos" tag → "video" (singularized)
	if _, ok := groups["video"]; !ok {
		t.Error("expected group 'video'")
	}
	// "Avatars" tag → "avatar"
	if _, ok := groups["avatar"]; !ok {
		t.Error("expected group 'avatar'")
	}
	// "Assets" tag → "asset"
	if _, ok := groups["asset"]; !ok {
		t.Error("expected group 'asset'")
	}
}

func TestGroupEndpoints_TerminalVerbs(t *testing.T) {
	doc := loadGroupTestSpec(t)
	examples := loadTestExamples(t)
	groups, _, err := GroupEndpoints(doc, examples)
	if err != nil {
		t.Fatalf("GroupEndpoints: %v", err)
	}

	names := make(map[string]bool)
	for _, s := range groups["video"] {
		names[s.Name] = true
	}

	expected := []string{"list", "create", "get", "delete", "caption get"}
	for _, name := range expected {
		if !names[name] {
			t.Errorf("missing video command %q, got %v", name, names)
		}
	}
}

func TestGroupEndpoints_QueryFlags(t *testing.T) {
	doc := loadGroupTestSpec(t)
	examples := loadTestExamples(t)
	groups, _, err := GroupEndpoints(doc, examples)
	if err != nil {
		t.Fatalf("GroupEndpoints: %v", err)
	}
	for _, s := range groups["video"] {
		if s.Name != "list" {
			continue
		}
		for _, flag := range s.Flags {
			if flag.Name == "limit" {
				if flag.Type != "int" {
					t.Errorf("limit type = %q, want 'int'", flag.Type)
				}
				if flag.Source != "query" {
					t.Errorf("limit source = %q, want 'query'", flag.Source)
				}
				return
			}
		}
		t.Error("limit flag not found on video list")
	}
}

func TestGroupEndpoints_BodyFlagsSkipComplex(t *testing.T) {
	doc := loadGroupTestSpec(t)
	examples := loadTestExamples(t)
	groups, _, err := GroupEndpoints(doc, examples)
	if err != nil {
		t.Fatalf("GroupEndpoints: %v", err)
	}
	for _, s := range groups["video"] {
		if s.Name != "create" {
			continue
		}
		for _, flag := range s.Flags {
			if flag.JSONName == "settings" {
				t.Error("complex field 'settings' should not be a flag")
			}
		}
		return
	}
}

func TestGroupEndpoints_Schemas(t *testing.T) {
	doc := loadGroupTestSpec(t)
	examples := loadTestExamples(t)
	groups, _, err := GroupEndpoints(doc, examples)
	if err != nil {
		t.Fatalf("GroupEndpoints: %v", err)
	}

	for _, s := range groups["video"] {
		switch s.Name {
		case "create":
			if s.RequestSchema == "" {
				t.Fatal("video create RequestSchema is empty")
			}
		case "list":
			if s.ResponseSchema == "" {
				t.Fatal("video list ResponseSchema is empty")
			}
		}
	}
}

func TestGroupEndpoints_PathArgs(t *testing.T) {
	doc := loadGroupTestSpec(t)
	examples := loadTestExamples(t)
	groups, _, err := GroupEndpoints(doc, examples)
	if err != nil {
		t.Fatalf("GroupEndpoints: %v", err)
	}
	for _, s := range groups["video"] {
		if s.Name != "get" {
			continue
		}
		if len(s.Args) != 1 || s.Args[0].Param != "video_id" {
			t.Errorf("expected path arg for video_id, got %+v", s.Args)
		}
		return
	}
	t.Error("video get not found")
}

func TestGroupEndpoints_Pagination(t *testing.T) {
	doc := loadGroupTestSpec(t)
	examples := loadTestExamples(t)
	groups, _, err := GroupEndpoints(doc, examples)
	if err != nil {
		t.Fatalf("GroupEndpoints: %v", err)
	}
	for _, s := range groups["video"] {
		if s.Name != "list" {
			continue
		}
		if !s.Paginated {
			t.Error("Paginated = false, want true")
		}
		return
	}
	t.Error("video list not found")
}

func TestGroupEndpoints_Multipart(t *testing.T) {
	doc := loadGroupTestSpec(t)
	examples := loadTestExamples(t)
	groups, _, err := GroupEndpoints(doc, examples)
	if err != nil {
		t.Fatalf("GroupEndpoints: %v", err)
	}
	specs := groups["asset"]
	if len(specs) == 0 {
		t.Fatal("asset group not found")
	}
	spec := specs[0]
	if spec.BodyEncoding != "multipart" {
		t.Errorf("BodyEncoding = %q, want 'multipart'", spec.BodyEncoding)
	}
	// File field should have Source: "file", not "body"
	found := false
	for _, flag := range spec.Flags {
		if flag.Name == "file" {
			found = true
			if flag.Source != "file" {
				t.Errorf("file flag Source = %q, want 'file'", flag.Source)
			}
		}
	}
	if !found {
		t.Error("--file flag not found on asset create")
	}
}

func TestGroupEndpoints_Examples(t *testing.T) {
	doc := loadGroupTestSpec(t)
	examples := loadTestExamples(t)
	groups, _, err := GroupEndpoints(doc, examples)
	if err != nil {
		t.Fatalf("GroupEndpoints: %v", err)
	}
	for _, s := range groups["video"] {
		if s.Name == "list" && len(s.Examples) == 0 {
			t.Error("expected examples on video list")
		}
	}
}

func TestGroupEndpoints_XCliAction(t *testing.T) {
	doc := loadGroupTestSpec(t)
	examples := loadTestExamples(t)
	groups, _, err := GroupEndpoints(doc, examples)
	if err != nil {
		t.Fatalf("GroupEndpoints: %v", err)
	}
	// consent has x-cli-action: true — should NOT get "create" appended
	for _, s := range groups["avatar"] {
		if s.Name == "consent" {
			return
		}
		if s.Name == "consent create" {
			t.Error("x-cli-action endpoint should not get terminal verb")
			return
		}
	}
	t.Error("avatar consent not found")
}

func TestGroupEndpoints_SubGroupNaming(t *testing.T) {
	doc := loadGroupTestSpec(t)
	examples := loadTestExamples(t)
	groups, _, err := GroupEndpoints(doc, examples)
	if err != nil {
		t.Fatalf("GroupEndpoints: %v", err)
	}
	// GET /v3/videos/{video_id}/caption → "caption get" (sub-group + terminal verb)
	for _, s := range groups["video"] {
		if s.Name == "caption get" {
			return
		}
	}
	t.Error("video 'caption get' not found")
}

func TestGroupEndpoints_SingletonGetUsesGetVerb(t *testing.T) {
	doc := loadGroupTestSpec(t)
	examples := loadTestExamples(t)
	groups, _, err := GroupEndpoints(doc, examples)
	if err != nil {
		t.Fatalf("GroupEndpoints: %v", err)
	}
	for _, s := range groups["user"] {
		if s.Name == "me get" {
			return
		}
	}
	t.Error("user 'me get' not found")
}
