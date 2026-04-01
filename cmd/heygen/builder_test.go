package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/heygen-com/heygen-cli/internal/command"
	clierrors "github.com/heygen-com/heygen-cli/internal/errors"
	"github.com/heygen-com/heygen-cli/internal/output"
)

// videoListSpec mirrors the hand-written video list command as a Spec,
// proving the generic builder produces identical behavior.
var videoListSpec = &command.Spec{
	Group:    "video",
	Name:     "list",
	Summary:  "List videos",
	Endpoint: "/v3/videos",
	Method:   "GET",
	// TokenField/DataField for pagination (used by paginator, not tested here)
	TokenField: "next_token",
	DataField:  "data",
	Flags: []command.FlagSpec{
		{Name: "limit", Type: "int", Source: "query", JSONName: "limit"},
		{Name: "token", Type: "string", Source: "query", JSONName: "token"},
		{Name: "folder-id", Type: "string", Source: "query", JSONName: "folder_id"},
	},
	Examples: []string{"heygen video list --limit 10"},
}

func TestGenBuilder_VideoList_Success(t *testing.T) {
	srv := setupTestServer(t, map[string]testHandler{
		"GET /v3/videos": {
			StatusCode: 200,
			Body:       `{"data":[{"id":"v1","title":"Demo","status":"completed"}],"has_more":false,"next_token":null}`,
		},
	})
	defer srv.Close()

	// Build a Cobra tree using the generic builder instead of hand-written newVideoListCmd.
	res := runGenCommand(t, srv.URL, "test-key", videoListSpec, "list")

	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(res.Stdout), &parsed); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %s", err, res.Stdout)
	}
	data, ok := parsed["data"].([]any)
	if !ok {
		t.Fatalf("data field missing or not array: %v", parsed)
	}
	if len(data) != 1 {
		t.Errorf("data length = %d, want 1", len(data))
	}
}

func TestGenBuilder_VideoList_Flags(t *testing.T) {
	srv := setupTestServer(t, map[string]testHandler{
		"GET /v3/videos": {
			StatusCode: 200,
			Body:       `{"data":[],"has_more":false}`,
			ValidateRequest: func(t *testing.T, r *http.Request) {
				t.Helper()
				q := r.URL.Query()
				if got := q.Get("limit"); got != "25" {
					t.Errorf("limit = %q, want %q", got, "25")
				}
				if got := q.Get("folder_id"); got != "folder_abc" {
					t.Errorf("folder_id = %q, want %q", got, "folder_abc")
				}
				if got := q.Get("token"); got != "cursor_xyz" {
					t.Errorf("token = %q, want %q", got, "cursor_xyz")
				}
			},
		},
	})
	defer srv.Close()

	res := runGenCommand(t, srv.URL, "test-key", videoListSpec,
		"list", "--limit", "25", "--folder-id", "folder_abc", "--token", "cursor_xyz")

	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}
}

func TestGenBuilder_PostWithBodyFlags(t *testing.T) {
	var gotBody map[string]any

	srv := setupTestServer(t, map[string]testHandler{
		"POST /v3/webhooks/endpoints": {
			StatusCode: 200,
			Body:       `{"data":{"id":"ep_123"}}`,
			ValidateRequest: func(t *testing.T, r *http.Request) {
				t.Helper()
				body, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(body, &gotBody)
			},
		},
	})
	defer srv.Close()

	spec := &command.Spec{
		Group:        "webhook",
		Name:         "create",
		Summary:      "Create a webhook endpoint",
		Endpoint:     "/v3/webhooks/endpoints",
		Method:       "POST",
		BodyEncoding: "json",
		Flags: []command.FlagSpec{
			{Name: "url", Type: "string", Source: "body", JSONName: "url", Required: true},
			{Name: "entity-id", Type: "string", Source: "body", JSONName: "entity_id"},
		},
		Examples: []string{"heygen webhook create --url https://example.com/hook"},
	}

	res := runGenCommand(t, srv.URL, "test-key", spec,
		"create", "--url", "https://example.com/hook", "--entity-id", "proj_456")

	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}
	if gotBody["url"] != "https://example.com/hook" {
		t.Errorf("body.url = %v, want %q", gotBody["url"], "https://example.com/hook")
	}
	if gotBody["entity_id"] != "proj_456" {
		t.Errorf("body.entity_id = %v, want %q", gotBody["entity_id"], "proj_456")
	}
}

func TestGenBuilder_PathParam(t *testing.T) {
	srv := setupTestServer(t, map[string]testHandler{
		"GET /v3/videos/abc123": {
			StatusCode: 200,
			Body:       `{"data":{"id":"abc123","status":"completed"}}`,
		},
	})
	defer srv.Close()

	spec := &command.Spec{
		Group:    "video",
		Name:     "get",
		Summary:  "Get video details",
		Endpoint: "/v3/videos/{video_id}",
		Method:   "GET",
		Args: []command.ArgSpec{
			{Name: "video-id", Param: "video_id"},
		},
		Examples: []string{"heygen video get abc123"},
	}

	res := runGenCommand(t, srv.URL, "test-key", spec, "get", "abc123")

	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}
}

func TestGenBuilder_BodylessPost_NoBodySent(t *testing.T) {
	srv := setupTestServer(t, map[string]testHandler{
		"POST /v3/avatars": {
			StatusCode: 200,
			Body:       `{"data":{"id":"avatar_new"}}`,
			ValidateRequest: func(t *testing.T, r *http.Request) {
				t.Helper()
				body, _ := io.ReadAll(r.Body)
				if len(body) > 0 {
					t.Errorf("expected no body for bodyless POST, got %q", string(body))
				}
			},
		},
	})
	defer srv.Close()

	spec := &command.Spec{
		Group:    "avatar",
		Name:     "create",
		Summary:  "Create an avatar",
		Endpoint: "/v3/avatars",
		Method:   "POST",
		// No BodyEncoding, no Flags with Source:"body" — truly bodyless
		Examples: []string{"heygen avatar create"},
	}

	res := runGenCommand(t, srv.URL, "test-key", spec, "create")

	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}
}

func TestGenBuilder_EnumValidation(t *testing.T) {
	srv := setupTestServer(t, map[string]testHandler{})
	defer srv.Close()

	spec := &command.Spec{
		Group:    "voice",
		Name:     "list",
		Summary:  "List voices",
		Endpoint: "/v3/voices",
		Method:   "GET",
		Flags: []command.FlagSpec{
			{Name: "type", Type: "string", Source: "query", JSONName: "type", Enum: []string{"public", "private"}},
		},
		Examples: []string{"heygen voice list --type public"},
	}

	res := runGenCommand(t, srv.URL, "test-key", spec, "list", "--type", "invalid")

	if res.ExitCode != 2 {
		t.Errorf("ExitCode = %d, want 2 (usage error)\nstderr: %s", res.ExitCode, res.Stderr)
	}
}

func TestGenBuilder_NestedCommand_Executes(t *testing.T) {
	var gotBody map[string]any

	srv := setupTestServer(t, map[string]testHandler{
		"POST /v3/voices/speech": {
			StatusCode: 200,
			Body:       `{"data":{"id":"speech_123"}}`,
			ValidateRequest: func(t *testing.T, r *http.Request) {
				t.Helper()
				body, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(body, &gotBody)
			},
		},
	})
	defer srv.Close()

	spec := &command.Spec{
		Group:        "voice",
		Name:         "speech create",
		Summary:      "Create speech audio",
		Endpoint:     "/v3/voices/speech",
		Method:       "POST",
		BodyEncoding: "json",
		Flags: []command.FlagSpec{
			{Name: "text", Type: "string", Source: "body", JSONName: "text", Required: true},
			{Name: "voice-id", Type: "string", Source: "body", JSONName: "voice_id", Required: true},
		},
		Examples: []string{"heygen voice speech create --text 'Hello world' --voice-id en_male"},
	}

	res := runGenCommand(t, srv.URL, "test-key", spec, "speech", "create",
		"--text", "Hello world", "--voice-id", "en_male")

	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}
	if gotBody["text"] != "Hello world" {
		t.Errorf("body.text = %v, want %q", gotBody["text"], "Hello world")
	}
	if gotBody["voice_id"] != "en_male" {
		t.Errorf("body.voice_id = %v, want %q", gotBody["voice_id"], "en_male")
	}
}

func TestGenBuilder_DeepNestedCommand_Executes(t *testing.T) {
	var gotBody map[string]any

	srv := setupTestServer(t, map[string]testHandler{
		"POST /v3/video-agents/sessions/sess_123/messages": {
			StatusCode: 200,
			Body:       `{"data":{"id":"msg_123"}}`,
			ValidateRequest: func(t *testing.T, r *http.Request) {
				t.Helper()
				body, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(body, &gotBody)
			},
		},
	})
	defer srv.Close()

	spec := &command.Spec{
		Group:        "video-agent",
		Name:         "sessions messages create",
		Summary:      "Create a session message",
		Endpoint:     "/v3/video-agents/sessions/{session_id}/messages",
		Method:       "POST",
		BodyEncoding: "json",
		Args: []command.ArgSpec{
			{Name: "session-id", Param: "session_id"},
		},
		Flags: []command.FlagSpec{
			{Name: "message", Type: "string", Source: "body", JSONName: "message", Required: true},
		},
		Examples: []string{"heygen video-agent sessions messages create <session-id> --message 'Add intro'"},
	}

	res := runGenCommand(t, srv.URL, "test-key", spec, "sessions", "messages", "create",
		"sess_123", "--message", "Add intro")

	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}
	if gotBody["message"] != "Add intro" {
		t.Errorf("body.message = %v, want %q", gotBody["message"], "Add intro")
	}
}

func TestGenBuilder_IntermediateHelp_ShowsChildCommands(t *testing.T) {
	spec := &command.Spec{
		Group:        "voice",
		Name:         "speech create",
		Summary:      "Create speech audio",
		Endpoint:     "/v3/voices/speech",
		Method:       "POST",
		BodyEncoding: "json",
		Flags: []command.FlagSpec{
			{Name: "text", Type: "string", Source: "body", JSONName: "text", Required: true},
		},
		Examples: []string{"heygen voice speech create --text 'Hello world'"},
	}

	res := runGeneratedRootCommand(t, "http://example.test", "test-key",
		map[string][]*command.Spec{"voice": {spec}},
		"voice", "speech", "--help")

	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}
	if !strings.Contains(res.Stdout, "Usage:\n  heygen voice speech [command]") {
		t.Errorf("help missing nested usage\nstdout: %s", res.Stdout)
	}
	if !strings.Contains(res.Stdout, "Available Commands:") || !strings.Contains(res.Stdout, "create") {
		t.Errorf("help missing child command listing\nstdout: %s", res.Stdout)
	}
	if strings.Contains(res.Stdout, "--text") {
		t.Errorf("intermediate help should not show leaf flags\nstdout: %s", res.Stdout)
	}
}

func TestGenBuilder_MultipartFileFlag_RoutesToInvocationFilePath(t *testing.T) {
	tmpFile, err := os.CreateTemp(t.TempDir(), "upload-*.txt")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer tmpFile.Close()
	if _, err := tmpFile.WriteString("hello upload"); err != nil {
		t.Fatalf("WriteString: %v", err)
	}

	srv := setupTestServer(t, map[string]testHandler{
		"POST /v3/assets": {
			StatusCode: 200,
			Body:       `{"data":{"id":"asset_123"}}`,
			ValidateRequest: func(t *testing.T, r *http.Request) {
				t.Helper()
				if got := r.Header.Get("Content-Type"); !strings.HasPrefix(got, "multipart/form-data;") {
					t.Errorf("Content-Type = %q, want multipart/form-data", got)
				}
				body, _ := io.ReadAll(r.Body)
				if !strings.Contains(string(body), "hello upload") {
					t.Errorf("multipart body missing file content: %q", string(body))
				}
			},
		},
	})
	defer srv.Close()

	spec := &command.Spec{
		Group:        "asset",
		Name:         "create",
		Summary:      "Upload an asset",
		Endpoint:     "/v3/assets",
		Method:       "POST",
		BodyEncoding: "multipart",
		Flags: []command.FlagSpec{
			{Name: "file", Type: "string", Source: "file", JSONName: "file", Required: true},
		},
		Examples: []string{"heygen asset create --file ./video.mp4"},
	}

	res := runGenCommand(t, srv.URL, "test-key", spec, "create", "--file", tmpFile.Name())

	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}
}

// runGenCommand builds a Cobra tree using the generic builder and executes it,
// similar to runCommand but using buildCobraCommand instead of the hand-written commands.
//
// It creates a fresh root command (with PersistentPreRunE that sets up the client)
// and adds the spec-built command under a group. The cmdContext is shared between
// PersistentPreRunE (which sets ctx.client) and the spec command (which uses it).
func runGenCommand(t *testing.T, serverURL, apiKey string, spec *command.Spec, args ...string) cmdResult {
	t.Helper()

	return runGeneratedRootCommand(t, serverURL, apiKey, map[string][]*command.Spec{
		spec.Group: {spec},
	}, append([]string{spec.Group}, args...)...)
}

func runGeneratedRootCommand(t *testing.T, serverURL, apiKey string, groups map[string][]*command.Spec, args ...string) cmdResult {
	t.Helper()

	var stdout, stderr bytes.Buffer
	formatter := output.NewJSONFormatter(&stdout, &stderr)

	t.Setenv("HEYGEN_API_KEY", apiKey)
	t.Setenv("HEYGEN_API_BASE", serverURL)

	root := newRootCmdWithSpecs("test", formatter, groups)
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(args)

	err := root.Execute()

	var exitCode int
	if err != nil {
		var cliErr *clierrors.CLIError
		if errors.As(err, &cliErr) {
			formatter.Error(cliErr)
			exitCode = cliErr.ExitCode
		} else {
			wrapped := classifyError(err)
			formatter.Error(wrapped)
			exitCode = wrapped.ExitCode
		}
	}

	return cmdResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
	}
}
