package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/heygen-com/heygen-cli/internal/command"
)

// These tests exercise the builder integration: that the clidesc overlay is
// applied to the rendered Cobra command surface (--help, --response-schema)
// and that non-overridden text falls through unchanged. The overlay resolvers
// themselves are unit-tested in internal/clidesc.

// TestDescriptionOverride_OperationLong verifies that a command whose endpoint
// has a description override renders the override as Long help (`--help`), not
// the generated spec.Description.
func TestDescriptionOverride_OperationLong(t *testing.T) {
	// /v3/voices/clone POST is overridden. The generated description cites
	// "GET /v3/voices/{voice_clone_id}"; the override replaces it with the CLI
	// `heygen voice get` framing.
	spec := &command.Spec{
		Group:       "voice",
		Name:        "clone create",
		Summary:     "Create a voice clone",
		Description: "Creates a voice clone from an audio file. Returns a voice_clone_id that can be polled via GET /v3/voices/{voice_clone_id} until the status is 'complete'.",
		Endpoint:    "/v3/voices/clone",
		Method:      "POST",
		Examples:    []string{"heygen voice clone create --request-schema"},
	}

	res := runGenCommand(t, "http://example.test", "test-key", spec, "clone", "create", "--help")
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}
	if !strings.Contains(res.Stdout, "poll it with `heygen voice get <voice-clone-id>`") {
		t.Fatalf("Long help did not render override:\n%s", res.Stdout)
	}
	if strings.Contains(res.Stdout, "GET /v3/voices/{voice_clone_id}") {
		t.Fatalf("Long help still shows generated HTTP-framed text:\n%s", res.Stdout)
	}
}

// TestDescriptionOverride_FlagHelp verifies that an overridden flag renders the
// override usage text, while a non-overridden flag on the same command falls
// through to its generated FlagSpec.Help.
func TestDescriptionOverride_FlagHelp(t *testing.T) {
	spec := &command.Spec{
		Group:        "video-translate",
		Name:         "create",
		Summary:      "Create video translation",
		Endpoint:     "/v3/video-translations",
		Method:       "POST",
		BodyEncoding: "json",
		Flags: []command.FlagSpec{
			// Overridden flag — generated help cites GET /v3/video-translations/languages.
			{Name: "output-languages", Type: "string-slice", Source: "body", JSONName: "output_languages", Help: "Use GET /v3/video-translations/languages for valid values."},
			// Non-overridden flag — should keep its generated help verbatim.
			{Name: "title", Type: "string", Source: "body", JSONName: "title", Help: "Optional title for the translation."},
		},
		Examples: []string{"heygen video-translate create --output-languages es"},
	}

	res := runGenCommand(t, "http://example.test", "test-key", spec, "create", "--help")
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}
	if !strings.Contains(res.Stdout, "Run 'heygen video-translate languages list' for valid values") {
		t.Fatalf("overridden flag help not rendered:\n%s", res.Stdout)
	}
	if strings.Contains(res.Stdout, "GET /v3/video-translations/languages") {
		t.Fatalf("overridden flag still shows generated HTTP text:\n%s", res.Stdout)
	}
	if !strings.Contains(res.Stdout, "Optional title for the translation.") {
		t.Fatalf("non-overridden flag did not fall through to generated help:\n%s", res.Stdout)
	}
}

// TestDescriptionOverride_ResponseSchemaField verifies that a response-schema
// field with an override renders the override description in
// `--response-schema` output, while a sibling field with no override is
// untouched.
func TestDescriptionOverride_ResponseSchemaField(t *testing.T) {
	// /v3/videos/{video_id} GET overrides video_url and captioned_video_url
	// but not subtitle_url.
	spec := &command.Spec{
		Group:    "video",
		Name:     "get",
		Summary:  "Get video",
		Endpoint: "/v3/videos/{video_id}",
		Method:   "GET",
		Args:     []command.ArgSpec{{Name: "video-id", Param: "video_id"}},
		ResponseSchema: `{
  "properties": {
    "data": {
      "properties": {
        "video_url": {"description": "Presigned URL to download the video file", "type": "string"},
        "subtitle_url": {"description": "Presigned URL to download the SRT subtitle file", "type": "string"}
      }
    }
  }
}`,
		Examples: []string{"heygen video get <video-id>"},
	}

	res := runGenCommand(t, "http://example.test", "", spec, "get", "vid_123", "--response-schema")
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}

	var doc struct {
		Properties struct {
			Data struct {
				Properties map[string]struct {
					Description string `json:"description"`
				} `json:"properties"`
			} `json:"data"`
		} `json:"properties"`
	}
	if err := json.Unmarshal([]byte(res.Stdout), &doc); err != nil {
		t.Fatalf("response-schema output not valid JSON: %v\n%s", err, res.Stdout)
	}
	props := doc.Properties.Data.Properties
	if got := props["video_url"].Description; !strings.Contains(got, "heygen video download <video-id>") {
		t.Fatalf("video_url description not overridden: %q", got)
	}
	if got := props["subtitle_url"].Description; got != "Presigned URL to download the SRT subtitle file" {
		t.Fatalf("subtitle_url should fall through unchanged, got: %q", got)
	}
}

// TestDescriptionOverride_Fallthrough verifies that a command on an endpoint
// with NO override keeps its generated Short, Long, and flag help verbatim.
func TestDescriptionOverride_Fallthrough(t *testing.T) {
	// /v3/videos GET (video list) has no override.
	spec := &command.Spec{
		Group:       "video",
		Name:        "list",
		Summary:     "List videos",
		Description: "Returns a paginated list of all videos in the account.",
		Endpoint:    "/v3/videos",
		Method:      "GET",
		Paginated:   true,
		Flags: []command.FlagSpec{
			{Name: "limit", Type: "int", Source: "query", JSONName: "limit", Help: "Maximum number of videos to return."},
		},
		Examples: []string{"heygen video list --limit 10"},
	}

	res := runGenCommand(t, "http://example.test", "test-key", spec, "list", "--help")
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}
	if !strings.Contains(res.Stdout, "Returns a paginated list of all videos in the account.") {
		t.Fatalf("non-overridden Long help should be the generated text:\n%s", res.Stdout)
	}
	if !strings.Contains(res.Stdout, "Maximum number of videos to return.") {
		t.Fatalf("non-overridden flag help should be the generated text:\n%s", res.Stdout)
	}
}
