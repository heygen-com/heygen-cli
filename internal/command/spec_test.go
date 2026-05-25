package command

import (
	"errors"
	"strings"
	"testing"

	clierrors "github.com/heygen-com/heygen-cli/internal/errors"
	"github.com/spf13/cobra"
)

// helperCmd creates a Cobra command with flags registered from the spec,
// then simulates flag parsing with the given args.
//
// The flag.Default field is honored for string types so ForceSend semantics
// can be exercised in tests without pulling in the cmd/heygen builder. The
// rest of the registration mirrors registerFlag (cmd/heygen/builder.go) at
// the level of detail these tests need.
func helperCmd(t *testing.T, spec *Spec, args []string) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{Use: "test", RunE: func(cmd *cobra.Command, args []string) error { return nil }}
	for _, f := range spec.Flags {
		switch f.Type {
		case "int":
			cmd.Flags().Int(f.Name, 0, f.Help)
		case "bool":
			cmd.Flags().Bool(f.Name, f.Default == "true", f.Help)
		case "float64":
			cmd.Flags().Float64(f.Name, 0, f.Help)
		case "string-slice":
			cmd.Flags().StringSlice(f.Name, nil, f.Help)
		default:
			cmd.Flags().String(f.Name, f.Default, f.Help)
		}
	}
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("failed to parse flags: %v", err)
	}
	return cmd
}

func TestBuildInvocation_QueryParamFlag(t *testing.T) {
	spec := &Spec{
		Flags: []FlagSpec{
			{Name: "limit", Type: "int", Source: "query", JSONName: "limit"},
		},
	}
	cmd := helperCmd(t, spec, []string{"--limit", "10"})

	inv, err := spec.BuildInvocation(cmd, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := inv.QueryParams.Get("limit"); got != "10" {
		t.Errorf("QueryParams[limit] = %q, want %q", got, "10")
	}
}

func TestBuildInvocation_BodyFieldFlag(t *testing.T) {
	spec := &Spec{
		Flags: []FlagSpec{
			{Name: "title", Type: "string", Source: "body", JSONName: "title"},
		},
	}
	cmd := helperCmd(t, spec, []string{"--title", "My Video"})

	inv, err := spec.BuildInvocation(cmd, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inv.Body == nil {
		t.Fatal("expected Body to be non-nil")
	}
	if inv.Body["title"] != "My Video" {
		t.Errorf("Body[title] = %v, want %q", inv.Body["title"], "My Video")
	}
}

func TestBuildInvocation_PathParamArg(t *testing.T) {
	spec := &Spec{
		Args: []ArgSpec{
			{Name: "video-id", Param: "video_id"},
		},
	}
	cmd := helperCmd(t, spec, nil)

	inv, err := spec.BuildInvocation(cmd, []string{"abc123"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inv.PathParams["video_id"] != "abc123" {
		t.Errorf("PathParams[video_id] = %q, want %q", inv.PathParams["video_id"], "abc123")
	}
}

func TestBuildInvocation_UnchangedFlagOmitted(t *testing.T) {
	spec := &Spec{
		Flags: []FlagSpec{
			{Name: "limit", Type: "int", Source: "query", JSONName: "limit"},
			{Name: "token", Type: "string", Source: "query", JSONName: "token"},
		},
	}
	// Only set --limit, leave --token unset
	cmd := helperCmd(t, spec, []string{"--limit", "5"})

	inv, err := spec.BuildInvocation(cmd, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := inv.QueryParams.Get("limit"); got != "5" {
		t.Errorf("QueryParams[limit] = %q, want %q", got, "5")
	}
	if got := inv.QueryParams.Get("token"); got != "" {
		t.Errorf("QueryParams[token] = %q, want empty (unset)", got)
	}
}

func TestBuildInvocation_EnumValidation(t *testing.T) {
	spec := &Spec{
		Flags: []FlagSpec{
			{Name: "type", Type: "string", Source: "query", JSONName: "type", Enum: []string{"public", "private"}},
		},
	}
	cmd := helperCmd(t, spec, []string{"--type", "invalid"})

	_, err := spec.BuildInvocation(cmd, nil, nil)
	if err == nil {
		t.Fatal("expected error for invalid enum value, got nil")
	}
	var cliErr *clierrors.CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("expected *CLIError, got %T", err)
	}
	if cliErr.ExitCode != clierrors.ExitUsage {
		t.Errorf("ExitCode = %d, want %d", cliErr.ExitCode, clierrors.ExitUsage)
	}
}

func TestBuildInvocation_EnumValid(t *testing.T) {
	spec := &Spec{
		Flags: []FlagSpec{
			{Name: "type", Type: "string", Source: "query", JSONName: "type", Enum: []string{"public", "private"}},
		},
	}
	cmd := helperCmd(t, spec, []string{"--type", "public"})

	inv, err := spec.BuildInvocation(cmd, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := inv.QueryParams.Get("type"); got != "public" {
		t.Errorf("QueryParams[type] = %q, want %q", got, "public")
	}
}

func TestBuildInvocation_MinMaxValidation(t *testing.T) {
	min, max := 1, 100
	spec := &Spec{
		Flags: []FlagSpec{
			{Name: "limit", Type: "int", Source: "query", JSONName: "limit", Min: &min, Max: &max},
		},
	}

	tests := []struct {
		name    string
		val     string
		wantErr bool
	}{
		{"below min", "0", true},
		{"above max", "999", true},
		{"at min", "1", false},
		{"at max", "100", false},
		{"in range", "50", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := helperCmd(t, spec, []string{"--limit", tt.val})
			_, err := spec.BuildInvocation(cmd, nil, nil)
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestBuildInvocation_DataBase(t *testing.T) {
	spec := &Spec{
		Flags: []FlagSpec{
			{Name: "title", Type: "string", Source: "body", JSONName: "title"},
		},
	}
	cmd := helperCmd(t, spec, nil) // no flags set

	data := map[string]any{
		"title": "From JSON",
		"video": map[string]any{"type": "url", "url": "https://example.com"},
	}

	inv, err := spec.BuildInvocation(cmd, nil, data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inv.Body["title"] != "From JSON" {
		t.Errorf("Body[title] = %v, want %q", inv.Body["title"], "From JSON")
	}
	// Complex field preserved
	if inv.Body["video"] == nil {
		t.Error("Body[video] should be preserved from -d/--data")
	}
}

func TestBuildInvocation_FlagOverridesData(t *testing.T) {
	spec := &Spec{
		Flags: []FlagSpec{
			{Name: "title", Type: "string", Source: "body", JSONName: "title"},
		},
	}
	cmd := helperCmd(t, spec, []string{"--title", "From Flag"})

	data := map[string]any{
		"title": "From JSON",
		"video": map[string]any{"type": "url"},
	}

	inv, err := spec.BuildInvocation(cmd, nil, data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Flag wins over -d/--data
	if inv.Body["title"] != "From Flag" {
		t.Errorf("Body[title] = %v, want %q (flag should override)", inv.Body["title"], "From Flag")
	}
	// Non-overlapping fields from -d/--data preserved
	if inv.Body["video"] == nil {
		t.Error("Body[video] should be preserved from -d/--data")
	}
}

func TestBuildInvocation_NoBodyWhenNoContent(t *testing.T) {
	spec := &Spec{
		Flags: []FlagSpec{
			{Name: "limit", Type: "int", Source: "query", JSONName: "limit"},
		},
	}
	cmd := helperCmd(t, spec, []string{"--limit", "10"})

	inv, err := spec.BuildInvocation(cmd, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inv.Body != nil {
		t.Errorf("Body should be nil for query-only command, got %v", inv.Body)
	}
}

func TestBuildInvocation_StringSliceFlag(t *testing.T) {
	spec := &Spec{
		Flags: []FlagSpec{
			{Name: "events", Type: "string-slice", Source: "body", JSONName: "events"},
		},
	}
	cmd := helperCmd(t, spec, []string{"--events", "a.success,b.fail"})

	inv, err := spec.BuildInvocation(cmd, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	events, ok := inv.Body["events"].([]string)
	if !ok {
		t.Fatalf("Body[events] type = %T, want []string", inv.Body["events"])
	}
	if len(events) != 2 {
		t.Errorf("events length = %d, want 2", len(events))
	}
}

func TestBuildInvocation_StringSliceEnumValidation(t *testing.T) {
	spec := &Spec{
		Flags: []FlagSpec{
			{Name: "events", Type: "string-slice", Source: "body", JSONName: "events", Enum: []string{"avatar_video.success", "avatar_video.fail"}},
		},
	}

	cmd := helperCmd(t, spec, []string{"--events", "avatar_video.success,bogus"})

	_, err := spec.BuildInvocation(cmd, nil, nil)
	if err == nil {
		t.Fatal("expected enum validation error, got nil")
	}
	var cliErr *clierrors.CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("expected *CLIError, got %T", err)
	}
	if cliErr.ExitCode != clierrors.ExitUsage {
		t.Fatalf("ExitCode = %d, want %d", cliErr.ExitCode, clierrors.ExitUsage)
	}
	if got := cliErr.Message; got == "" || !strings.Contains(got, `value "bogus"`) {
		t.Fatalf("message = %q, want invalid slice value", got)
	}
}

func TestBuildInvocation_StringSliceEnumValid(t *testing.T) {
	spec := &Spec{
		Flags: []FlagSpec{
			{Name: "events", Type: "string-slice", Source: "body", JSONName: "events", Enum: []string{"avatar_video.success", "avatar_video.fail"}},
		},
	}

	cmd := helperCmd(t, spec, []string{"--events", "avatar_video.success,avatar_video.fail"})

	inv, err := spec.BuildInvocation(cmd, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	events, ok := inv.Body["events"].([]string)
	if !ok || len(events) != 2 {
		t.Fatalf("Body[events] = %#v, want 2 validated string-slice values", inv.Body["events"])
	}
}

// ---------------------------------------------------------------------------
// ForceSend — CLI-specific default that must reach the server even when the
// user doesn't pass the flag.
//
// Motivation: aspect_ratio's API default is "16:9" but the CLI's effective
// default is "auto" via x-cli-default. Without ForceSend, BuildInvocation
// skips any flag the user didn't change, the request goes out with no
// aspect_ratio, and the API applies "16:9" — making "auto" a help-text-only
// fiction. ForceSend keeps the gate open so the body actually carries "auto".
// ---------------------------------------------------------------------------

func TestBuildInvocation_ForceSendWritesBodyDefaultWhenFlagOmitted(t *testing.T) {
	spec := &Spec{
		Flags: []FlagSpec{
			{Name: "aspect-ratio", Type: "string", Source: "body", JSONName: "aspect_ratio", Default: "auto", ForceSend: true},
		},
	}
	cmd := helperCmd(t, spec, nil) // user omits --aspect-ratio

	inv, err := spec.BuildInvocation(cmd, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inv.Body == nil {
		t.Fatal("Body should be populated by ForceSend flag")
	}
	if inv.Body["aspect_ratio"] != "auto" {
		t.Errorf("Body[aspect_ratio] = %v, want %q", inv.Body["aspect_ratio"], "auto")
	}
}

func TestBuildInvocation_ForceSendUserValueWins(t *testing.T) {
	// User-supplied value must still win over the ForceSend default; ForceSend
	// only matters when the user is silent.
	spec := &Spec{
		Flags: []FlagSpec{
			{Name: "aspect-ratio", Type: "string", Source: "body", JSONName: "aspect_ratio", Default: "auto", ForceSend: true},
		},
	}
	cmd := helperCmd(t, spec, []string{"--aspect-ratio", "9:16"})

	inv, err := spec.BuildInvocation(cmd, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inv.Body["aspect_ratio"] != "9:16" {
		t.Errorf("Body[aspect_ratio] = %v, want %q (user value should win)", inv.Body["aspect_ratio"], "9:16")
	}
}

func TestBuildInvocation_ForceSendForQueryParam(t *testing.T) {
	// Symmetry check: ForceSend works for query params too, not just body.
	spec := &Spec{
		Flags: []FlagSpec{
			{Name: "scope", Type: "string", Source: "query", JSONName: "scope", Default: "user", ForceSend: true},
		},
	}
	cmd := helperCmd(t, spec, nil)

	inv, err := spec.BuildInvocation(cmd, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := inv.QueryParams.Get("scope"); got != "user" {
		t.Errorf("QueryParams[scope] = %q, want %q", got, "user")
	}
}

func TestBuildInvocation_NonForceSendDefaultStaysOmitted(t *testing.T) {
	// Regression guard: ordinary OpenAPI defaults (no x-cli-default) must keep
	// the existing omit-unless-changed behavior. Otherwise the CLI would start
	// echoing every server default back to the server on every request.
	spec := &Spec{
		Flags: []FlagSpec{
			{Name: "fps", Type: "int", Source: "body", JSONName: "fps", Default: "30", ForceSend: false},
		},
	}
	cmd := helperCmd(t, spec, nil)

	inv, err := spec.BuildInvocation(cmd, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inv.Body != nil {
		if _, present := inv.Body["fps"]; present {
			t.Errorf("Body should not carry fps when the user omitted it; got Body=%v", inv.Body)
		}
	}
}

func TestBuildInvocation_ForceSendValidatesAgainstEnum(t *testing.T) {
	// The ForceSend default must itself satisfy the enum constraint. A bad
	// codegen output (default value outside enum) should fail validation, not
	// silently ship an invalid request. This is a defense against future
	// codegen bugs where the x-cli-default value doesn't match the enum.
	spec := &Spec{
		Flags: []FlagSpec{
			{
				Name:      "aspect-ratio",
				Type:      "string",
				Source:    "body",
				JSONName:  "aspect_ratio",
				Default:   "bogus",
				ForceSend: true,
				Enum:      []string{"16:9", "9:16", "auto"},
			},
		},
	}
	cmd := helperCmd(t, spec, nil)

	if _, err := spec.BuildInvocation(cmd, nil, nil); err == nil {
		t.Fatal("expected enum validation error for ForceSend default outside enum, got nil")
	}
}
