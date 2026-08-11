package main

import (
	"slices"
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

// TestGroupEndpoints_BodyFlagsRespectXCliDefault locks in the per-surface default
// override. Background: “aspect_ratio“ defaults to “16:9“ over the HTTP API
// (existing callers rely on it), but agent-driven CLI/MCP flows are better off
// defaulting to “auto“ so the output canvas tracks the source orientation.
// EF authors signal this via “json_schema_extra={"x-cli-default": "auto"}“,
// which lands in the spec next to the API “default“. Codegen must surface the
// override, not the API value.
//
// The test spec mirrors that shape on the “aspect_ratio“ field plus a control
// field (“fps“) that has only “default“ so we don't accidentally break the
// fallback path.
func TestGroupEndpoints_BodyFlagsRespectXCliDefault(t *testing.T) {
	doc := loadGroupTestSpec(t)
	examples := loadTestExamples(t)
	groups, _, err := GroupEndpoints(doc, examples)
	if err != nil {
		t.Fatalf("GroupEndpoints: %v", err)
	}
	var sawOverride, sawFallback bool
	for _, s := range groups["video"] {
		if s.Name != "create" {
			continue
		}
		for _, flag := range s.Flags {
			switch flag.JSONName {
			case "aspect_ratio":
				sawOverride = true
				if flag.Default != "auto" {
					t.Errorf("aspect_ratio: x-cli-default should win over default; got Default=%q, want %q", flag.Default, "auto")
				}
				// SendDefaultWhenOmitted must be true so BuildInvocation
				// materializes the CLI default into the request body when the
				// user omits the flag — otherwise the server applies its own
				// default ("16:9") and the CLI's "auto" is a help-text-only
				// fiction.
				if !flag.SendDefaultWhenOmitted {
					t.Error("aspect_ratio: SendDefaultWhenOmitted should be true for x-cli-default-sourced flags")
				}
			case "fps":
				sawFallback = true
				if flag.Default != "30" {
					t.Errorf("fps: with no x-cli-default the schema default should be used; got Default=%q, want %q", flag.Default, "30")
				}
				// Ordinary OpenAPI defaults keep the existing omit-unless-changed
				// behavior — the CLI shouldn't start echoing every server default
				// back. Only x-cli-default flips SendDefaultWhenOmitted.
				if flag.SendDefaultWhenOmitted {
					t.Error("fps: SendDefaultWhenOmitted must stay false for ordinary schema defaults")
				}
			}
		}
		if !sawOverride {
			t.Error("aspect_ratio flag not found on video create")
		}
		if !sawFallback {
			t.Error("fps flag (fallback control) not found on video create")
		}
		return
	}
	t.Error("video create not found")
}

// TestSchemaCliDefault locks the precedence rules on the helper itself so a
// future refactor that splits or inlines it can't silently flip behavior.
//
// fromExtension is the signal that downstream codegen uses to set
// FlagSpec.SendDefaultWhenOmitted — true only when the value came from x-cli-default, not
// from an ordinary OpenAPI default.
func TestSchemaCliDefault(t *testing.T) {
	cases := []struct {
		name              string
		schema            *openapi3.Schema
		wantValue         interface{}
		wantOk            bool
		wantFromExtension bool
	}{
		{"nil schema", nil, nil, false, false},
		{"no default, no extension", &openapi3.Schema{}, nil, false, false},
		{
			"default only",
			&openapi3.Schema{Default: "16:9"},
			"16:9",
			true,
			false,
		},
		{
			"x-cli-default overrides default",
			&openapi3.Schema{Default: "16:9", Extensions: map[string]interface{}{"x-cli-default": "auto"}},
			"auto",
			true,
			true,
		},
		{
			"x-cli-default with no default",
			&openapi3.Schema{Extensions: map[string]interface{}{"x-cli-default": "auto"}},
			"auto",
			true,
			true,
		},
		{
			"x-mcp-default is ignored — that's the MCP codegen's concern",
			&openapi3.Schema{Default: "16:9", Extensions: map[string]interface{}{"x-mcp-default": "auto"}},
			"16:9",
			true,
			false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok, fromExt := schemaCliDefault(tc.schema)
			if ok != tc.wantOk {
				t.Fatalf("ok mismatch: got %v, want %v", ok, tc.wantOk)
			}
			if got != tc.wantValue {
				t.Errorf("value mismatch: got %v, want %v", got, tc.wantValue)
			}
			if fromExt != tc.wantFromExtension {
				t.Errorf("fromExtension mismatch: got %v, want %v", fromExt, tc.wantFromExtension)
			}
		})
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

func commandNames(specs []*command.Spec) []string {
	names := make([]string, 0, len(specs))
	for _, s := range specs {
		names = append(names, s.Name)
	}
	return names
}

// A group's primary root just restates the group name, so it is dropped:
// "/v3/videos/{video_id}" in group "video" is "get", not "videos get".
func TestGroupEndpoints_PrimaryRootDropped(t *testing.T) {
	doc := loadGroupTestSpec(t)
	groups, _, err := GroupEndpoints(doc, loadTestExamples(t))
	if err != nil {
		t.Fatalf("GroupEndpoints: %v", err)
	}
	for _, name := range commandNames(groups["video"]) {
		if strings.HasPrefix(name, "videos ") {
			t.Errorf("command %q kept the shared path root; want it dropped", name)
		}
	}
}

// When one tag spans several resources the root is the only thing telling them
// apart, so it survives as a sub-group — minus the group prefix, so the noun
// doesn't double ("brand brand-kits get").
func TestGroupEndpoints_MultiResourceGroupKeepsPathRoot(t *testing.T) {
	doc := loadGroupTestSpec(t)
	groups, _, err := GroupEndpoints(doc, loadTestExamples(t))
	if err != nil {
		t.Fatalf("GroupEndpoints: %v", err)
	}
	got := commandNames(groups["brand"])
	slices.Sort(got)
	want := []string{"glossaries get", "glossaries list", "kits get", "kits list"}
	if !slices.Equal(got, want) {
		t.Errorf("brand commands = %v, want %v", got, want)
	}
}

// A root that normalizes to the group name is redundant on its own terms, so one
// foreign-rooted endpoint joining a group must not rename the commands already
// in it. Guards a silent mass-rename of a shipped public interface.
func TestGroupEndpoints_ForeignRootDoesNotRenameGroup(t *testing.T) {
	doc := loadGroupTestSpec(t)
	groups, _, err := GroupEndpoints(doc, loadTestExamples(t))
	if err != nil {
		t.Fatalf("GroupEndpoints: %v", err)
	}
	got := commandNames(groups["asset"])
	slices.Sort(got)
	// "create" from /v3/assets keeps its name despite /v3/uploads sharing the tag.
	want := []string{"create", "uploads list"}
	if !slices.Equal(got, want) {
		t.Errorf("asset commands = %v, want %v", got, want)
	}
}

// Same protection as above, for a group whose primary root is pinned rather than
// derived. Normalization can't recognize "video-translations" as belonging to
// "video-translate", so this is the case that regressed when the primary root was
// inferred from whether a group's endpoints agreed.
func TestGroupEndpoints_ForeignRootDoesNotRenamePinnedGroup(t *testing.T) {
	doc := loadGroupTestSpec(t)
	groups, _, err := GroupEndpoints(doc, loadTestExamples(t))
	if err != nil {
		t.Fatalf("GroupEndpoints: %v", err)
	}
	got := commandNames(groups["video-translate"])
	slices.Sort(got)
	want := []string{"create", "dubs list"}
	if !slices.Equal(got, want) {
		t.Errorf("video-translate commands = %v, want %v", got, want)
	}
}

// groupEndpointsFromYAML runs the real pipeline over an inline spec, so the
// guards below are pinned through GroupEndpoints rather than by calling a
// validator directly — removing the call site must fail these tests.
func groupEndpointsFromYAML(t *testing.T, spec string) error {
	t.Helper()
	doc, err := openapi3.NewLoader().LoadFromData([]byte(spec))
	if err != nil {
		t.Fatalf("loading inline spec: %v", err)
	}
	_, _, err = GroupEndpoints(doc, Examples{})
	return err
}

const ambiguousRootsSpec = `
openapi: 3.0.0
info: {title: t, version: "1"}
paths:
  /v3/assets:
    post: {tags: [Assets], summary: Create an asset, responses: {"201": {description: Created}}}
  /v3/asset:
    get: {tags: [Assets], summary: List assets, responses: {"200": {description: OK}}}
`

// Both roots normalize to "asset" so both look primary, and the differing verbs
// mean the generated names never collide. Codegen must reject the mapping.
func TestGroupEndpoints_RejectsTwoPrimaryRoots(t *testing.T) {
	err := groupEndpointsFromYAML(t, ambiguousRootsSpec)
	if err == nil {
		t.Fatal("expected an error for two primary roots in one group")
	}
	for _, want := range []string{`"asset"`, `"assets"`, "groupPrimaryRoots"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %s", err, want)
		}
	}
}

const collidingSubGroupSpec = `
openapi: 3.0.0
info: {title: t, version: "1"}
paths:
  /v3/brand-glossaries:
    get: {tags: [Brand], summary: List glossaries, responses: {"200": {description: OK}}}
  /v3/brand-kits:
    get: {tags: [Brand], summary: List kits, responses: {"200": {description: OK}}}
  /v3/kits:
    post: {tags: [Brand], summary: Create a kit, responses: {"201": {description: Created}}}
`

// Prefix trimming is many-to-one too: "brand-kits" trims to "kits", and a
// sibling root literally named "kits" keeps it. Different verbs again mean no
// name collision, so the mapping check is what catches it.
func TestGroupEndpoints_RejectsCollidingSubGroups(t *testing.T) {
	err := groupEndpointsFromYAML(t, collidingSubGroupSpec)
	if err == nil {
		t.Fatal("expected an error for two roots mapping onto one sub-group")
	}
	for _, want := range []string{`"brand-kits"`, `"kits"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %s", err, want)
		}
	}
}

const unversionedPrefixSpec = `
openapi: 3.0.0
info: {title: t, version: "1"}
paths:
  /api/v1/videos:
    get: {tags: [Videos], summary: List videos, responses: {"200": {description: OK}}}
`

// Naming skips exactly one leading segment. A two-segment prefix would otherwise
// treat "api" as the root and ride "v1 videos" onto every command in the spec,
// with nothing to collide against.
func TestGroupEndpoints_RejectsUnversionedPrefix(t *testing.T) {
	err := groupEndpointsFromYAML(t, unversionedPrefixSpec)
	if err == nil {
		t.Fatal("expected an error for a path not starting with a version segment")
	}
	if !strings.Contains(err.Error(), `"api"`) {
		t.Errorf("error %q should name the unexpected prefix segment", err)
	}
}

const paramRootSpec = `
openapi: 3.0.0
info: {title: t, version: "1"}
paths:
  /v3/{video_id}:
    get:
      tags: [Videos]
      summary: Get a video
      parameters:
        - name: video_id
          in: path
          required: true
          schema: {type: string}
      responses: {"200": {description: OK}}
`

// The one shape where buildSpec's root reconcile and validateRootSubGroups would
// disagree: the builder skips a {param} root, the validator would treat it as a
// sub-group token. Reject it so the two can't diverge, and so the command isn't a
// bare verb naming no resource.
func TestGroupEndpoints_RejectsParameterAsResourceRoot(t *testing.T) {
	err := groupEndpointsFromYAML(t, paramRootSpec)
	if err == nil {
		t.Fatal("expected an error for a {param} in the resource-root position")
	}
	if !strings.Contains(err.Error(), "{video_id}") {
		t.Errorf("error %q should name the offending segment", err)
	}
}

const noResourceSegmentSpec = `
openapi: 3.0.0
info: {title: t, version: "1"}
paths:
  /widgets:
    get: {tags: [Widgets], summary: List widgets, responses: {"200": {description: OK}}}
`

// Without a resource segment the command is a bare verb, which can alias a
// sibling resource instead of colliding with it.
func TestGroupEndpoints_RejectsPathWithoutResourceSegment(t *testing.T) {
	err := groupEndpointsFromYAML(t, noResourceSegmentSpec)
	if err == nil {
		t.Fatal("expected an error for a path with no resource segment")
	}
	if !strings.Contains(err.Error(), "/widgets") {
		t.Errorf("error %q should name the offending path", err)
	}
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

// TestGroupEndpoints_BodyFlagsCarryDeprecated locks in that OpenAPI `deprecated`
// reaches the FlagSpec. Without it the CLI advertises a superseded field as a
// normal flag: `brand_voice_id` carried `deprecated: true` in the spec for
// months while `--brand-voice-id` stayed fully visible in `--help`.
//
// `fps` is the control. It is an ordinary field on the same body, so if the
// marker ever leaked onto every flag rather than the tagged one, this case goes
// red while the positive case stays green.
func TestGroupEndpoints_BodyFlagsCarryDeprecated(t *testing.T) {
	doc := loadGroupTestSpec(t)
	examples := loadTestExamples(t)
	groups, _, err := GroupEndpoints(doc, examples)
	if err != nil {
		t.Fatalf("GroupEndpoints: %v", err)
	}
	var sawDeprecated, sawControl bool
	for _, s := range groups["video"] {
		if s.Name != "create" {
			continue
		}
		for _, flag := range s.Flags {
			switch flag.JSONName {
			case "legacy_caption":
				sawDeprecated = true
				if !flag.Deprecated {
					t.Error("legacy_caption: schema `deprecated: true` should set FlagSpec.Deprecated")
				}
			case "fps":
				sawControl = true
				if flag.Deprecated {
					t.Error("fps: an undeprecated field must not be marked Deprecated")
				}
			}
		}
	}
	if !sawDeprecated {
		t.Error("legacy_caption flag not found on video create — a deprecated field must still become a flag, not be dropped")
	}
	if !sawControl {
		t.Error("fps control flag not found on video create")
	}
}

// Query parameters carry `deprecated` on the parameter object rather than on a
// schema property, and grouper reads it from a different place than the body
// path does. Without this, a refactor that split the two paths could drop the
// query side silently, since the body test would stay green.
func TestGroupEndpoints_QueryFlagsCarryDeprecated(t *testing.T) {
	doc := loadGroupTestSpec(t)
	examples := loadTestExamples(t)
	groups, _, err := GroupEndpoints(doc, examples)
	if err != nil {
		t.Fatalf("GroupEndpoints: %v", err)
	}
	var sawDeprecated, sawControl bool
	for _, s := range groups["avatar"] {
		for _, flag := range s.Flags {
			switch flag.JSONName {
			case "legacy_filter":
				sawDeprecated = true
				if !flag.Deprecated {
					t.Error("legacy_filter: `deprecated: true` on a query parameter should set FlagSpec.Deprecated")
				}
			case "ownership":
				sawControl = true
				if flag.Deprecated {
					t.Error("ownership: an undeprecated query parameter must not be marked Deprecated")
				}
			}
		}
	}
	if !sawDeprecated {
		t.Error("legacy_filter flag not found — a deprecated query param must still become a flag")
	}
	if !sawControl {
		t.Error("ownership control flag not found")
	}
}
