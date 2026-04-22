package main

import (
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/heygen-com/heygen-cli/internal/command"
)

func nullableRef(schema *openapi3.Schema) *openapi3.Schema {
	return &openapi3.Schema{
		AnyOf: openapi3.SchemaRefs{
			openapi3.NewSchemaRef("", schema),
			openapi3.NewSchemaRef("", &openapi3.Schema{Type: &openapi3.Types{"null"}}),
		},
	}
}

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

func TestGroupEndpoints_BodyFlagsSkipHiddenFields(t *testing.T) {
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
			if flag.JSONName == "watermark_s3_key" {
				t.Error("x-cli-visible=false field 'watermark_s3_key' should not be a flag")
			}
		}
		return
	}
	t.Error("video create not found")
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

func TestDeriveCommandName_Override(t *testing.T) {
	// Override with no sub-groups: just the override name
	got := deriveCommandName("/v3/video-agents/{session_id}", "POST", nil, []string{"{session_id}"}, &openapi3.Operation{})
	if got != "send" {
		t.Fatalf("deriveCommandName = %q, want %q", got, "send")
	}
}

func TestDeriveCommandName_OverrideNested(t *testing.T) {
	// Override with sub-groups: preserve sub-groups, replace terminal verb
	old := nameOverrides
	nameOverrides = map[string]string{
		"POST /v3/widgets/parts/{part_id}/details": "inspect",
	}
	defer func() { nameOverrides = old }()

	got := deriveCommandName("/v3/widgets/parts/{part_id}/details", "POST", []string{"parts", "details"}, []string{"parts", "{part_id}", "details"}, &openapi3.Operation{})
	if got != "parts details inspect" {
		t.Fatalf("deriveCommandName = %q, want %q", got, "parts details inspect")
	}
}

func TestValidateCommandNames_DetectsConflict(t *testing.T) {
	groups := command.Groups{
		"widget": {
			&command.Spec{Name: "create", Method: "POST", Endpoint: "/v3/widgets"},
			&command.Spec{Name: "create", Method: "POST", Endpoint: "/v3/widgets/{widget_id}"},
		},
	}
	err := validateCommandNames(groups)
	if err == nil {
		t.Fatal("expected error for duplicate names")
	}
	if !strings.Contains(err.Error(), "naming conflict") {
		t.Fatalf("error = %q, want naming conflict", err.Error())
	}
	if !strings.Contains(err.Error(), "nameOverrides") {
		t.Fatalf("error = %q, want nameOverrides hint", err.Error())
	}
}

func TestValidateCommandNames_NoConflict(t *testing.T) {
	groups := command.Groups{
		"widget": {
			&command.Spec{Name: "create", Method: "POST", Endpoint: "/v3/widgets"},
			&command.Spec{Name: "send", Method: "POST", Endpoint: "/v3/widgets/{widget_id}"},
		},
	}
	if err := validateCommandNames(groups); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUnwrapNullableType_String(t *testing.T) {
	unwrapped := unwrapNullableType(nullableRef(openapi3.NewStringSchema()))
	if unwrapped == nil {
		t.Fatal("unwrapNullableType returned nil")
	}
	if got := mapSchemaType(unwrapped); got != "string" {
		t.Fatalf("mapSchemaType = %q, want string", got)
	}
}

func TestUnwrapNullableType_Bool(t *testing.T) {
	unwrapped := unwrapNullableType(nullableRef(openapi3.NewBoolSchema()))
	if unwrapped == nil {
		t.Fatal("unwrapNullableType returned nil")
	}
	if got := mapSchemaType(unwrapped); got != "bool" {
		t.Fatalf("mapSchemaType = %q, want bool", got)
	}
}

func TestUnwrapNullableType_PrimitiveArray(t *testing.T) {
	schema := openapi3.NewArraySchema().WithItems(openapi3.NewStringSchema())
	unwrapped := unwrapNullableType(nullableRef(schema))
	if unwrapped == nil {
		t.Fatal("unwrapNullableType returned nil")
	}
	if got := mapSchemaType(unwrapped); got != "string-slice" {
		t.Fatalf("mapSchemaType = %q, want string-slice", got)
	}
}

func TestUnwrapNullableType_PrimitiveArrayWithEnum(t *testing.T) {
	item := openapi3.NewStringSchema().WithEnum("alpha", "beta")
	schema := openapi3.NewArraySchema().WithItems(item)
	unwrapped := unwrapNullableType(nullableRef(schema))
	if unwrapped == nil {
		t.Fatal("unwrapNullableType returned nil")
	}
	if got := schemaEnum(nullableRef(schema)); len(got) != 2 || got[0] != "alpha" || got[1] != "beta" {
		t.Fatalf("schemaEnum = %v, want [alpha beta]", got)
	}
}

func TestUnwrapNullableType_ArrayOfObjects(t *testing.T) {
	schema := openapi3.NewArraySchema().WithItems(openapi3.NewObjectSchema())
	if got := unwrapNullableType(nullableRef(schema)); got != nil {
		t.Fatalf("unwrapNullableType = %v, want nil", got)
	}
}

func TestUnwrapNullableType_Object(t *testing.T) {
	if got := unwrapNullableType(nullableRef(openapi3.NewObjectSchema())); got != nil {
		t.Fatalf("unwrapNullableType = %v, want nil", got)
	}
}

func TestUnwrapNullableType_PolymorphicUnion(t *testing.T) {
	schema := &openapi3.Schema{
		AnyOf: openapi3.SchemaRefs{
			openapi3.NewSchemaRef("", openapi3.NewObjectSchema()),
			openapi3.NewSchemaRef("", openapi3.NewObjectSchema()),
		},
	}
	if got := unwrapNullableType(schema); got != nil {
		t.Fatalf("unwrapNullableType = %v, want nil", got)
	}
}

func TestUnwrapNullableType_MixedPrimitives(t *testing.T) {
	schema := &openapi3.Schema{
		AnyOf: openapi3.SchemaRefs{
			openapi3.NewSchemaRef("", openapi3.NewStringSchema()),
			openapi3.NewSchemaRef("", openapi3.NewIntegerSchema()),
			openapi3.NewSchemaRef("", &openapi3.Schema{Type: &openapi3.Types{"null"}}),
		},
	}
	if got := unwrapNullableType(schema); got != nil {
		t.Fatalf("unwrapNullableType = %v, want nil", got)
	}
}

func TestSchemaEnum_NullableEnum(t *testing.T) {
	schema := nullableRef(openapi3.NewStringSchema().WithEnum("landscape", "portrait"))
	got := schemaEnum(schema)
	if len(got) != 2 || got[0] != "landscape" || got[1] != "portrait" {
		t.Fatalf("schemaEnum = %v, want [landscape portrait]", got)
	}
}

func TestSchemaEnum_NullableArrayEnum(t *testing.T) {
	item := openapi3.NewStringSchema().WithEnum("alpha", "beta")
	schema := nullableRef(openapi3.NewArraySchema().WithItems(item))
	got := schemaEnum(schema)
	if len(got) != 2 || got[0] != "alpha" || got[1] != "beta" {
		t.Fatalf("schemaEnum = %v, want [alpha beta]", got)
	}
}

func TestGroupEndpoints_NullableFieldsPromoted(t *testing.T) {
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

		flags := make(map[string]command.FlagSpec)
		for _, flag := range s.Flags {
			flags[flag.Name] = flag
		}

		title, ok := flags["title"]
		if !ok {
			t.Fatal("missing nullable string flag title")
		}
		if title.Type != "string" || title.Source != "body" {
			t.Fatalf("title = %+v, want string body flag", title)
		}

		categories, ok := flags["categories"]
		if !ok {
			t.Fatal("missing nullable primitive array flag categories")
		}
		if categories.Type != "string-slice" || categories.Source != "body" {
			t.Fatalf("categories = %+v, want string-slice body flag", categories)
		}
		if len(categories.Enum) != 2 || categories.Enum[0] != "marketing" || categories.Enum[1] != "social" {
			t.Fatalf("categories enum = %v, want [marketing social]", categories.Enum)
		}
		return
	}

	t.Fatal("video create not found")
}
