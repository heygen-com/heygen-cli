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

	"github.com/heygen-com/heygen-cli/internal/analytics"
	"github.com/heygen-com/heygen-cli/internal/command"
	clierrors "github.com/heygen-com/heygen-cli/internal/errors"
)

// videoListSpec mirrors the hand-written video list command as a Spec,
// proving the generic builder produces identical behavior.
var videoListSpec = &command.Spec{
	Group:     "video",
	Name:      "list",
	Summary:   "List videos",
	Endpoint:  "/v3/videos",
	Method:    "GET",
	Paginated: true,
	Flags: []command.FlagSpec{
		{Name: "limit", Type: "int", Source: "query", JSONName: "limit"},
		{Name: "token", Type: "string", Source: "query", JSONName: "token"},
		{Name: "folder-id", Type: "string", Source: "query", JSONName: "folder_id"},
	},
	Examples: []string{"heygen video list --limit 10"},
}

// videoCreateWaitSpec has Group/Name matching pollConfigs["video/create"],
// so the builder picks up PollConfig at call time — no need to set it here.
var videoCreateWaitSpec = &command.Spec{
	Group:        "video",
	Name:         "create",
	Summary:      "Create a video",
	Endpoint:     "/v3/videos",
	Method:       "POST",
	BodyEncoding: "json",
	Examples:     []string{"heygen video create --wait"},
}

var videoCreateSchemaSpec = &command.Spec{
	Group:          "video",
	Name:           "create",
	Summary:        "Create a video",
	Endpoint:       "/v3/videos",
	Method:         "POST",
	BodyEncoding:   "json",
	RequestSchema:  "{\n  \"type\": \"object\"\n}",
	ResponseSchema: "{\n  \"type\": \"object\",\n  \"properties\": {\n    \"data\": {\n      \"type\": \"object\"\n    }\n  }\n}",
	Examples:       []string{"heygen video create --request-schema"},
}

var videoGetSchemaSpec = &command.Spec{
	Group:          "video",
	Name:           "get",
	Summary:        "Get video",
	Endpoint:       "/v3/videos/{video_id}",
	Method:         "GET",
	ResponseSchema: "{\n  \"type\": \"object\"\n}",
	Args: []command.ArgSpec{
		{Name: "video-id", Param: "video_id"},
	},
	Examples: []string{"heygen video get <video-id>"},
}

var videoTranslateSchemaSpec = &command.Spec{
	Group:         "video-translate",
	Name:          "create",
	Summary:       "Create video translation",
	Endpoint:      "/v3/video-translations",
	Method:        "POST",
	BodyEncoding:  "json",
	RequestSchema: "{\n  \"type\": \"object\"\n}",
	Flags: []command.FlagSpec{
		{Name: "output-languages", Type: "string-slice", Source: "body", JSONName: "output_languages", Required: true},
	},
	Examples: []string{"heygen video-translate create --request-schema"},
}

var webhookEndpointUpdateSchemaSpec = &command.Spec{
	Group:         "webhook",
	Name:          "endpoints update",
	Summary:       "Update webhook endpoint",
	Endpoint:      "/v3/webhooks/endpoints/{endpoint_id}",
	Method:        "PATCH",
	RequestSchema: "{\n  \"type\": \"object\"\n}",
	Args: []command.ArgSpec{
		{Name: "endpoint-id", Param: "endpoint_id"},
	},
	Examples: []string{"heygen webhook endpoints update <endpoint-id> --request-schema"},
}

var videoAgentWaitSpec = &command.Spec{
	Group:        "video-agent",
	Name:         "create",
	Summary:      "Create video with Video Agent",
	Endpoint:     "/v3/video-agents",
	Method:       "POST",
	BodyEncoding: "json",
	Examples:     []string{"heygen video-agent create --wait"},
}

var videoDeleteSpec = &command.Spec{
	Group:       "video",
	Name:        "delete",
	Summary:     "Delete a video",
	Endpoint:    "/v3/videos/{video_id}",
	Method:      "DELETE",
	Destructive: true,
	Args: []command.ArgSpec{
		{Name: "video-id", Param: "video_id"},
	},
	Examples: []string{"heygen video delete <video-id>"},
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

func TestGenBuilder_VideoCreate_RequestSchema(t *testing.T) {
	res := runGenCommand(t, "http://example.test", "", videoCreateSchemaSpec, "create", "--request-schema")

	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}
	if res.Stdout != videoCreateSchemaSpec.RequestSchema+"\n" {
		t.Fatalf("stdout = %q, want request schema", res.Stdout)
	}
	if res.Stderr != "" {
		t.Fatalf("stderr = %q, want empty", res.Stderr)
	}
}

func TestGenBuilder_VideoGet_ResponseSchema(t *testing.T) {
	res := runGenCommand(t, "http://example.test", "", videoGetSchemaSpec, "get", "vid_123", "--response-schema")

	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}
	if res.Stdout != videoGetSchemaSpec.ResponseSchema+"\n" {
		t.Fatalf("stdout = %q, want response schema", res.Stdout)
	}
}

func TestGenBuilder_VideoList_NoRequestSchema(t *testing.T) {
	res := runGenCommand(t, "http://example.test", "test-key", videoListSpec, "list", "--request-schema")

	if res.ExitCode != 2 {
		t.Fatalf("ExitCode = %d, want 2\nstderr: %s", res.ExitCode, res.Stderr)
	}
	if !strings.Contains(res.Stderr, "unknown flag: --request-schema") {
		t.Fatalf("stderr = %s, want unknown flag error", res.Stderr)
	}
}

func TestGenBuilder_VideoCreate_SchemaSkipsAuth(t *testing.T) {
	res := runGenCommand(t, "http://example.test", "", videoCreateSchemaSpec, "create", "--response-schema")

	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}
	if res.Stdout != videoCreateSchemaSpec.ResponseSchema+"\n" {
		t.Fatalf("stdout = %q, want response schema", res.Stdout)
	}
}

func TestGenBuilder_SchemaBypassesRequiredFlags(t *testing.T) {
	res := runGenCommand(t, "http://example.test", "", videoTranslateSchemaSpec, "create", "--request-schema")

	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}
	if res.Stdout != videoTranslateSchemaSpec.RequestSchema+"\n" {
		t.Fatalf("stdout = %q, want request schema", res.Stdout)
	}
	if res.Stderr != "" {
		t.Fatalf("stderr = %q, want empty", res.Stderr)
	}
}

func TestGenBuilder_SchemaBypassesArgs(t *testing.T) {
	res := runGenCommand(t, "http://example.test", "", webhookEndpointUpdateSchemaSpec, "endpoints", "update", "--request-schema")

	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}
	if res.Stdout != webhookEndpointUpdateSchemaSpec.RequestSchema+"\n" {
		t.Fatalf("stdout = %q, want request schema", res.Stdout)
	}
	if res.Stderr != "" {
		t.Fatalf("stderr = %q, want empty", res.Stderr)
	}
}

func TestGenBuilder_RequiredFlagsStillValidatedWithoutSchema(t *testing.T) {
	res := runGenCommand(t, "http://example.test", "test-key", videoTranslateSchemaSpec, "create")

	if res.ExitCode != 2 {
		t.Fatalf("ExitCode = %d, want 2\nstderr: %s", res.ExitCode, res.Stderr)
	}
	if !strings.Contains(res.Stderr, "required flag(s)") || !strings.Contains(res.Stderr, "output-languages") {
		t.Fatalf("stderr = %q, want required flag error", res.Stderr)
	}
}

func TestGenBuilder_ArgsStillValidatedWithoutSchema(t *testing.T) {
	res := runGenCommand(t, "http://example.test", "", webhookEndpointUpdateSchemaSpec, "endpoints", "update")

	if res.ExitCode != 2 {
		t.Fatalf("ExitCode = %d, want 2\nstderr: %s", res.ExitCode, res.Stderr)
	}
	if !strings.Contains(res.Stderr, "accepts 1 arg(s), received 0") {
		t.Fatalf("stderr = %q, want positional arg error", res.Stderr)
	}
}

func TestGenBuilder_DataSatisfiesRequiredBodyFlags(t *testing.T) {
	srv := setupTestServer(t, map[string]testHandler{
		"POST /v3/video-translations": {
			StatusCode: 200,
			Body:       `{"data":{"id":"vt_123"}}`,
		},
	})
	defer srv.Close()

	spec := &command.Spec{
		Group:        "video-translate",
		Name:         "create",
		Summary:      "Create translation",
		Endpoint:     "/v3/video-translations",
		Method:       "POST",
		BodyEncoding: "json",
		Flags: []command.FlagSpec{
			{Name: "output-languages", Type: "string-slice", Source: "body", JSONName: "output_languages", Required: true},
		},
		Examples: []string{"heygen video-translate create -d '{\"output_languages\":[\"es\"]}'"},
	}

	res := runGenCommand(t, srv.URL, "test-key", spec, "create", "-d", `{"output_languages":["es"]}`)

	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}
}

func TestGenBuilder_DataDoesNotBypassRequiredQueryFlags(t *testing.T) {
	spec := &command.Spec{
		Group:        "video",
		Name:         "create",
		Summary:      "Create video",
		Endpoint:     "/v3/videos",
		Method:       "POST",
		BodyEncoding: "json",
		Flags: []command.FlagSpec{
			{Name: "folder-id", Type: "string", Source: "query", JSONName: "folder_id", Required: true},
			{Name: "title", Type: "string", Source: "body", JSONName: "title"},
		},
		Examples: []string{"heygen video create --folder-id folder_123"},
	}

	res := runGenCommand(t, "http://example.test", "test-key", spec, "create", "-d", `{"title":"Demo"}`)

	if res.ExitCode != clierrors.ExitUsage {
		t.Fatalf("ExitCode = %d, want %d\nstderr: %s", res.ExitCode, clierrors.ExitUsage, res.Stderr)
	}
	if !strings.Contains(res.Stderr, "required flag(s)") || !strings.Contains(res.Stderr, "folder-id") {
		t.Fatalf("stderr = %q, want required query flag error", res.Stderr)
	}
}

func TestGenBuilder_HelpShowsRequiredAnnotation(t *testing.T) {
	spec := &command.Spec{
		Group:        "video-agent",
		Name:         "create",
		Summary:      "Create video with Video Agent",
		Endpoint:     "/v3/video-agents",
		Method:       "POST",
		BodyEncoding: "json",
		Flags: []command.FlagSpec{
			{Name: "prompt", Type: "string", Source: "body", JSONName: "prompt", Help: "The message/prompt for video generation", Required: true},
			{Name: "orientation", Type: "string", Source: "body", JSONName: "orientation", Help: "Video orientation", Enum: []string{"landscape", "portrait"}},
		},
		Examples: []string{"heygen video-agent create --prompt \"Hello\""},
	}

	res := runGenCommand(t, "http://example.test", "test-key", spec, "create", "--help")

	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}
	if !strings.Contains(res.Stdout, "The message/prompt for video generation (required)") {
		t.Fatalf("stdout = %s, want required annotation", res.Stdout)
	}
	if !strings.Contains(res.Stdout, "Video orientation (allowed: landscape, portrait)") {
		t.Fatalf("stdout = %s, want allowed annotation", res.Stdout)
	}
	if strings.Contains(res.Stdout, "Video orientation (required)") {
		t.Fatalf("stdout = %s, optional flag should not be marked required", res.Stdout)
	}
}

func TestGenBuilder_DestructiveForceSkipsPrompt(t *testing.T) {
	var deleteCalls int
	srv := setupTestServer(t, map[string]testHandler{
		"DELETE /v3/videos/vid_123": {
			StatusCode: 200,
			Body:       `{"data":{"id":"vid_123"}}`,
			ValidateRequest: func(t *testing.T, r *http.Request) {
				t.Helper()
				deleteCalls++
			},
		},
	})
	defer srv.Close()

	res := runGenCommand(t, srv.URL, "test-key", videoDeleteSpec, "delete", "vid_123", "--force")

	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}
	if deleteCalls != 1 {
		t.Fatalf("deleteCalls = %d, want 1", deleteCalls)
	}
	if strings.Contains(res.Stderr, "Continue? [y/N]") {
		t.Fatalf("stderr = %q, want no prompt", res.Stderr)
	}
}

func TestGenBuilder_DestructiveNonTTYRequiresForce(t *testing.T) {
	srv := setupTestServer(t, map[string]testHandler{
		"DELETE /v3/videos/vid_123": {
			StatusCode: 200,
			Body:       `{"data":{"id":"vid_123"}}`,
		},
	})
	defer srv.Close()

	res := runGenCommand(t, srv.URL, "test-key", videoDeleteSpec, "delete", "vid_123")

	if res.ExitCode != 1 {
		t.Fatalf("ExitCode = %d, want 1\nstderr: %s", res.ExitCode, res.Stderr)
	}
	if !strings.Contains(res.Stderr, "confirmation_required") {
		t.Fatalf("stderr = %q, want confirmation_required error", res.Stderr)
	}
	if !strings.Contains(res.Stderr, "--force") {
		t.Fatalf("stderr = %q, want --force hint", res.Stderr)
	}
}

func TestGenBuilder_DestructiveTTYYesConfirms(t *testing.T) {
	orig := stdinIsTerminalFunc
	stdinIsTerminalFunc = func() bool { return true }
	t.Cleanup(func() { stdinIsTerminalFunc = orig })

	var deleteCalls int
	srv := setupTestServer(t, map[string]testHandler{
		"DELETE /v3/videos/vid_123": {
			StatusCode: 200,
			Body:       `{"data":{"id":"vid_123"}}`,
			ValidateRequest: func(t *testing.T, r *http.Request) {
				t.Helper()
				deleteCalls++
			},
		},
	})
	defer srv.Close()

	res := runGenCommandWithInput(t, srv.URL, "test-key", videoDeleteSpec, strings.NewReader("y\n"), "delete", "vid_123")

	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}
	if deleteCalls != 1 {
		t.Fatalf("deleteCalls = %d, want 1", deleteCalls)
	}
	if !strings.Contains(res.Stderr, "Warning: Delete a video. Continue? [y/N]") {
		t.Fatalf("stderr = %q, want prompt", res.Stderr)
	}
}

func TestGenBuilder_DestructiveTTYNoCancels(t *testing.T) {
	orig := stdinIsTerminalFunc
	stdinIsTerminalFunc = func() bool { return true }
	t.Cleanup(func() { stdinIsTerminalFunc = orig })

	var deleteCalls int
	srv := setupTestServer(t, map[string]testHandler{
		"DELETE /v3/videos/vid_123": {
			StatusCode: 200,
			Body:       `{"data":{"id":"vid_123"}}`,
			ValidateRequest: func(t *testing.T, r *http.Request) {
				t.Helper()
				deleteCalls++
			},
		},
	})
	defer srv.Close()

	res := runGenCommandWithInput(t, srv.URL, "test-key", videoDeleteSpec, strings.NewReader("n\n"), "delete", "vid_123")

	if res.ExitCode != clierrors.ExitGeneral {
		t.Fatalf("ExitCode = %d, want %d\nstderr: %s", res.ExitCode, clierrors.ExitGeneral, res.Stderr)
	}
	if deleteCalls != 0 {
		t.Fatalf("deleteCalls = %d, want 0", deleteCalls)
	}
	if !strings.Contains(res.Stderr, `"code":"canceled"`) {
		t.Fatalf("stderr = %q, want canceled error code", res.Stderr)
	}
}

func TestGenBuilder_DestructiveTTYEmptyCancels(t *testing.T) {
	orig := stdinIsTerminalFunc
	stdinIsTerminalFunc = func() bool { return true }
	t.Cleanup(func() { stdinIsTerminalFunc = orig })

	var deleteCalls int
	srv := setupTestServer(t, map[string]testHandler{
		"DELETE /v3/videos/vid_123": {
			StatusCode: 200,
			Body:       `{"data":{"id":"vid_123"}}`,
			ValidateRequest: func(t *testing.T, r *http.Request) {
				t.Helper()
				deleteCalls++
			},
		},
	})
	defer srv.Close()

	res := runGenCommandWithInput(t, srv.URL, "test-key", videoDeleteSpec, strings.NewReader("\n"), "delete", "vid_123")

	if res.ExitCode != clierrors.ExitGeneral {
		t.Fatalf("ExitCode = %d, want %d\nstderr: %s", res.ExitCode, clierrors.ExitGeneral, res.Stderr)
	}
	if deleteCalls != 0 {
		t.Fatalf("deleteCalls = %d, want 0", deleteCalls)
	}
	if !strings.Contains(res.Stderr, `"code":"canceled"`) {
		t.Fatalf("stderr = %q, want canceled error code", res.Stderr)
	}
}

func TestGenBuilder_VideoCreate_Wait_Success(t *testing.T) {
	var statusCalls int
	srv := setupTestServer(t, map[string]testHandler{
		"POST /v3/videos": {
			StatusCode: 200,
			Body:       `{"data":{"video_id":"vid_123"}}`,
		},
		"GET /v3/videos/vid_123": {
			StatusCode: 200,
			ValidateRequest: func(t *testing.T, r *http.Request) {
				t.Helper()
				statusCalls++
			},
			Body: `{"data":{"video_id":"vid_123","status":"processing"}}`,
		},
	})
	defer srv.Close()

	originalHandler := srv.Config.Handler
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/v3/videos/vid_123" {
			statusCalls++
			w.WriteHeader(http.StatusOK)
			if statusCalls < 2 {
				_, _ = w.Write([]byte(`{"data":{"video_id":"vid_123","status":"processing"}}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":{"video_id":"vid_123","status":"completed","video_url":"https://cdn.test/video.mp4"}}`))
			return
		}
		originalHandler.ServeHTTP(w, r)
	})

	res := runGenCommand(t, srv.URL, "test-key", videoCreateWaitSpec, "create", "--wait", "-d", `{"test":true}`)

	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(res.Stdout), &parsed); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %s", err, res.Stdout)
	}
	data := parsed["data"].(map[string]any)
	if data["status"] != "completed" {
		t.Fatalf("status = %v, want completed", data["status"])
	}
	// JSON mode: no progress on stderr (keeps it machine-readable).
	// Progress is only emitted in --human mode.
	if strings.Contains(res.Stderr, "Polling:") {
		t.Fatalf("stderr should not contain progress in JSON mode: %s", res.Stderr)
	}
}

func TestGenBuilder_VideoCreate_Wait_Human_NonTTYFallback(t *testing.T) {
	var statusCalls int
	srv := setupTestServer(t, map[string]testHandler{
		"POST /v3/videos": {
			StatusCode: 200,
			Body:       `{"data":{"video_id":"vid_123"}}`,
		},
		"GET /v3/videos/vid_123": {
			StatusCode: 200,
			ValidateRequest: func(t *testing.T, r *http.Request) {
				t.Helper()
				statusCalls++
			},
			Body: `{"data":{"video_id":"vid_123","status":"processing"}}`,
		},
	})
	defer srv.Close()

	originalHandler := srv.Config.Handler
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/v3/videos/vid_123" {
			statusCalls++
			w.WriteHeader(http.StatusOK)
			if statusCalls < 2 {
				_, _ = w.Write([]byte(`{"data":{"video_id":"vid_123","status":"processing"}}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":{"video_id":"vid_123","status":"completed","video_url":"https://cdn.test/video.mp4"}}`))
			return
		}
		originalHandler.ServeHTTP(w, r)
	})

	res := runGenCommand(t, srv.URL, "test-key", videoCreateWaitSpec, "create", "--wait", "--human", "-d", `{"test":true}`)

	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}
	if !strings.Contains(res.Stderr, "Polling: status=processing") {
		t.Fatalf("stderr = %s, want plain-text non-TTY progress", res.Stderr)
	}
	if !strings.Contains(res.Stdout, "Status:") {
		t.Fatalf("stdout = %s, want human-formatted output", res.Stdout)
	}
}

func TestGenBuilder_VideoCreate_Wait_Failure(t *testing.T) {
	srv := setupTestServer(t, map[string]testHandler{
		"POST /v3/videos": {
			StatusCode: 200,
			Body:       `{"data":{"video_id":"vid_123"}}`,
		},
		"GET /v3/videos/vid_123": {
			StatusCode: 200,
			Body:       `{"data":{"video_id":"vid_123","status":"failed"}}`,
		},
	})
	defer srv.Close()

	res := runGenCommand(t, srv.URL, "test-key", videoCreateWaitSpec, "create", "--wait", "-d", `{"test":true}`)

	if res.ExitCode != 1 {
		t.Fatalf("ExitCode = %d, want 1\nstderr: %s", res.ExitCode, res.Stderr)
	}
	if !strings.Contains(res.Stderr, "operation reached terminal failure state: failed") {
		t.Fatalf("stderr = %s, want failure message", res.Stderr)
	}
	// Failure response should be output to stdout so users can see error details
	var parsed map[string]any
	if err := json.Unmarshal([]byte(res.Stdout), &parsed); err != nil {
		t.Fatalf("stdout should contain failure response: %v\nstdout: %s", err, res.Stdout)
	}
	data := parsed["data"].(map[string]any)
	if data["status"] != "failed" {
		t.Fatalf("status = %v, want failed", data["status"])
	}
}

func TestGenBuilder_VideoCreate_Wait_Timeout(t *testing.T) {
	srv := setupTestServer(t, map[string]testHandler{
		"POST /v3/videos": {
			StatusCode: 200,
			Body:       `{"data":{"video_id":"vid_123"}}`,
		},
		"GET /v3/videos/vid_123": {
			StatusCode: 200,
			Body:       `{"data":{"video_id":"vid_123","status":"processing"}}`,
		},
	})
	defer srv.Close()

	res := runGenCommand(t, srv.URL, "test-key", videoCreateWaitSpec, "create", "--wait", "--timeout", "20ms", "-d", `{"test":true}`)

	if res.ExitCode != clierrors.ExitTimeout {
		t.Fatalf("ExitCode = %d, want %d\nstderr: %s", res.ExitCode, clierrors.ExitTimeout, res.Stderr)
	}
	if !strings.Contains(res.Stderr, "polling timed out after 20ms") {
		t.Fatalf("stderr = %s, want timeout message", res.Stderr)
	}
	if !strings.Contains(res.Stderr, "heygen video get vid_123") {
		t.Fatalf("stderr = %s, want follow-up hint", res.Stderr)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(res.Stdout), &parsed); err != nil {
		t.Fatalf("stdout should contain partial response: %v\nstdout: %s", err, res.Stdout)
	}
	data := parsed["data"].(map[string]any)
	if data["status"] != "processing" {
		t.Fatalf("status = %v, want processing", data["status"])
	}
}

func TestGenBuilder_VideoCreate_Wait_TimeoutBeforeFirstPoll(t *testing.T) {
	srv := setupTestServer(t, map[string]testHandler{
		"POST /v3/videos": {
			StatusCode: 200,
			Body:       `{"data":{"video_id":"vid_123"}}`,
		},
		"GET /v3/videos/vid_123": {
			StatusCode: 200,
			ValidateRequest: func(t *testing.T, r *http.Request) {
				t.Helper()
				<-r.Context().Done()
			},
			Body: `{"data":{"video_id":"vid_123","status":"processing"}}`,
		},
	})
	defer srv.Close()

	res := runGenCommand(t, srv.URL, "test-key", videoCreateWaitSpec, "create", "--wait", "--timeout", "20ms", "-d", `{"test":true}`)

	if res.ExitCode != clierrors.ExitTimeout {
		t.Fatalf("ExitCode = %d, want %d\nstderr: %s", res.ExitCode, clierrors.ExitTimeout, res.Stderr)
	}
	if strings.TrimSpace(res.Stdout) != "" {
		t.Fatalf("stdout = %q, want empty when no status response was received", res.Stdout)
	}
}

func TestGenBuilder_VideoCreate_Wait_Timeout_Human(t *testing.T) {
	srv := setupTestServer(t, map[string]testHandler{
		"POST /v3/videos": {
			StatusCode: 200,
			Body:       `{"data":{"video_id":"vid_123"}}`,
		},
		"GET /v3/videos/vid_123": {
			StatusCode: 200,
			Body:       `{"data":{"video_id":"vid_123","status":"processing","created_at":1774712936}}`,
		},
	})
	defer srv.Close()

	res := runGenCommand(t, srv.URL, "test-key", videoCreateWaitSpec, "create", "--wait", "--timeout", "20ms", "--human", "-d", `{"test":true}`)

	if res.ExitCode != clierrors.ExitTimeout {
		t.Fatalf("ExitCode = %d, want %d\nstderr: %s", res.ExitCode, clierrors.ExitTimeout, res.Stderr)
	}
	if !strings.Contains(res.Stderr, "Polling: status=processing") {
		t.Fatalf("stderr = %s, want progress output", res.Stderr)
	}
	if !strings.Contains(res.Stderr, "Error: polling timed out after 20ms") {
		t.Fatalf("stderr = %s, want human timeout error", res.Stderr)
	}
	if !strings.Contains(res.Stderr, "Hint: heygen video get vid_123") {
		t.Fatalf("stderr = %s, want human hint", res.Stderr)
	}
	if !strings.Contains(res.Stdout, "Video Id") && !strings.Contains(res.Stdout, "Video ID") && !strings.Contains(res.Stdout, "video_id") {
		t.Fatalf("stdout = %s, want rendered partial result", res.Stdout)
	}
	if !strings.Contains(res.Stdout, "processing") {
		t.Fatalf("stdout = %s, want processing status", res.Stdout)
	}
}

func TestGenBuilder_VideoAgentCreate_Wait_Timeout_UsesVideoGetHint(t *testing.T) {
	srv := setupTestServer(t, map[string]testHandler{
		"POST /v3/video-agents": {
			StatusCode: 200,
			Body:       `{"data":{"session_id":"sess_123","video_id":"vid_123"}}`,
		},
		"GET /v3/videos/vid_123": {
			StatusCode: 200,
			Body:       `{"data":{"video_id":"vid_123","status":"processing"}}`,
		},
	})
	defer srv.Close()

	res := runGenCommand(t, srv.URL, "test-key", videoAgentWaitSpec, "create", "--wait", "--timeout", "20ms", "-d", `{"prompt":"test"}`)

	if res.ExitCode != clierrors.ExitTimeout {
		t.Fatalf("ExitCode = %d, want %d\nstderr: %s", res.ExitCode, clierrors.ExitTimeout, res.Stderr)
	}
	if !strings.Contains(res.Stderr, "heygen video get vid_123") {
		t.Fatalf("stderr = %s, want video get hint", res.Stderr)
	}
	if strings.Contains(res.Stderr, "video-agent get") {
		t.Fatalf("stderr = %s, should not suggest video-agent get", res.Stderr)
	}
}

func TestGenBuilder_VideoCreate_NoWait(t *testing.T) {
	var statusCalled bool
	srv := setupTestServer(t, map[string]testHandler{
		"POST /v3/videos": {
			StatusCode: 200,
			Body:       `{"data":{"video_id":"vid_123","status":"pending"}}`,
		},
		"GET /v3/videos/vid_123": {
			StatusCode: 200,
			ValidateRequest: func(t *testing.T, r *http.Request) {
				t.Helper()
				statusCalled = true
			},
			Body: `{"data":{"video_id":"vid_123","status":"completed"}}`,
		},
	})
	defer srv.Close()

	res := runGenCommand(t, srv.URL, "test-key", videoCreateWaitSpec, "create", "-d", `{"test":true}`)

	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}
	if statusCalled {
		t.Fatal("status endpoint should not be called without --wait")
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(res.Stdout), &parsed); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %s", err, res.Stdout)
	}
	data := parsed["data"].(map[string]any)
	if data["status"] != "pending" {
		t.Fatalf("status = %v, want pending", data["status"])
	}
}

func TestGenBuilder_VideoCreate_WaitNotAvailable(t *testing.T) {
	nonPollable := &command.Spec{
		Group:    "video",
		Name:     "get",
		Summary:  "Get video",
		Endpoint: "/v3/videos/{video_id}",
		Method:   "GET",
		Args: []command.ArgSpec{
			{Name: "video-id", Param: "video_id"},
		},
		Examples: []string{"heygen video get <video-id>"},
	}

	srv := setupTestServer(t, map[string]testHandler{})
	defer srv.Close()

	res := runGenCommand(t, srv.URL, "test-key", nonPollable, "get", "vid_123", "--wait")

	if res.ExitCode != 2 {
		t.Fatalf("ExitCode = %d, want 2\nstderr: %s", res.ExitCode, res.Stderr)
	}
	if !strings.Contains(res.Stderr, "unknown flag: --wait") {
		t.Fatalf("stderr = %s, want unknown flag error", res.Stderr)
	}
}

func TestGenBuilder_PostWithoutBody_RejectsEmptyRequest(t *testing.T) {
	requiredBodySpec := &command.Spec{
		Group:         "video",
		Name:          "create",
		Summary:       "Create a video",
		Endpoint:      "/v3/videos",
		Method:        "POST",
		BodyEncoding:  "json",
		RequestSchema: `{"type":"object","properties":{"title":{"type":"string"}},"required":["title"]}`,
		Examples:      []string{"heygen video create -d ..."},
	}

	srv := setupTestServer(t, map[string]testHandler{})
	defer srv.Close()

	res := runGenCommand(t, srv.URL, "test-key", requiredBodySpec, "create")

	if res.ExitCode != clierrors.ExitUsage {
		t.Fatalf("ExitCode = %d, want %d\nstderr: %s", res.ExitCode, clierrors.ExitUsage, res.Stderr)
	}
	if !strings.Contains(res.Stderr, "request body required") {
		t.Fatalf("stderr = %s, want 'request body required' message", res.Stderr)
	}
	if !strings.Contains(res.Stderr, "--request-schema") {
		t.Fatalf("stderr = %s, want --request-schema hint", res.Stderr)
	}
}

// POST commands with an intentionally empty request schema (e.g.
// video-agent stop — properties: {}, required: []) must not be rejected
// by the empty-body guard.
func TestGenBuilder_PostWithEmptySchema_AllowsEmptyBody(t *testing.T) {
	emptyBodySpec := &command.Spec{
		Group:        "video-agent",
		Name:         "stop",
		Summary:      "Stop Video Agent session",
		Endpoint:     "/v3/video-agents/{session_id}/stop",
		Method:       "POST",
		BodyEncoding: "json",
		RequestSchema: `{
			"description": "Request body for stopping a session (empty — no fields required).",
			"properties": {},
			"required": [],
			"type": "object"
		}`,
		Args: []command.ArgSpec{
			{Name: "session-id", Param: "session_id"},
		},
		Examples: []string{"heygen video-agent stop <session-id>"},
	}

	srv := setupTestServer(t, map[string]testHandler{
		"POST /v3/video-agents/sess_123/stop": {
			StatusCode: 200,
			Body:       `{"data":{"session_id":"sess_123"}}`,
		},
	})
	defer srv.Close()

	res := runGenCommand(t, srv.URL, "test-key", emptyBodySpec, "stop", "sess_123")

	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
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

func TestGenBuilder_WebhookEventsFlag(t *testing.T) {
	var gotBody map[string]any

	srv := setupTestServer(t, map[string]testHandler{
		"POST /v3/webhooks/endpoints": {
			StatusCode: 200,
			Body:       `{"data":{"endpoint_id":"ep_123"}}`,
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
		Name:         "endpoints create",
		Summary:      "Create endpoint",
		Endpoint:     "/v3/webhooks/endpoints",
		Method:       "POST",
		BodyEncoding: "json",
		Flags: []command.FlagSpec{
			{Name: "url", Type: "string", Source: "body", JSONName: "url", Required: true},
			{Name: "events", Type: "string-slice", Source: "body", JSONName: "events", Enum: []string{"avatar_video.success", "avatar_video.fail"}},
		},
		Examples: []string{"heygen webhook endpoints create --url https://example.com/hook --events avatar_video.success"},
	}

	res := runGenCommand(t, srv.URL, "test-key", spec, "endpoints", "create",
		"--url", "https://example.com/hook", "--events", "avatar_video.success")

	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}
	events, ok := gotBody["events"].([]any)
	if !ok || len(events) != 1 || events[0] != "avatar_video.success" {
		t.Fatalf("body.events = %#v, want [avatar_video.success]", gotBody["events"])
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
		"PUT /v3/video-translations/proofreads/pr_123/srt": {
			StatusCode: 200,
			Body:       `{"data":{"id":"pr_123"}}`,
			ValidateRequest: func(t *testing.T, r *http.Request) {
				t.Helper()
				body, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(body, &gotBody)
			},
		},
	})
	defer srv.Close()

	spec := &command.Spec{
		Group:        "video-translate",
		Name:         "proofreads srt update",
		Summary:      "Upload proofread SRT",
		Endpoint:     "/v3/video-translations/proofreads/{proofread_id}/srt",
		Method:       "PUT",
		BodyEncoding: "json",
		Args: []command.ArgSpec{
			{Name: "proofread-id", Param: "proofread_id"},
		},
		Flags: []command.FlagSpec{
			{Name: "content", Type: "string", Source: "body", JSONName: "content", Required: true},
		},
		Examples: []string{"heygen video-translate proofreads srt update <proofread-id> --content 'SRT data'"},
	}

	res := runGenCommand(t, srv.URL, "test-key", spec, "proofreads", "srt", "update",
		"pr_123", "--content", "SRT data")

	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}
	if gotBody["content"] != "SRT data" {
		t.Errorf("body.content = %v, want %q", gotBody["content"], "SRT data")
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
	if !strings.Contains(res.Stdout, "Usage:") || !strings.Contains(res.Stdout, "heygen voice speech [command]") {
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

	return runGenCommandWithInput(t, serverURL, apiKey, spec, nil, args...)
}

func runGenCommandWithInput(t *testing.T, serverURL, apiKey string, spec *command.Spec, stdin io.Reader, args ...string) cmdResult {
	t.Helper()

	return runGeneratedRootCommandWithInput(t, serverURL, apiKey, map[string][]*command.Spec{
		spec.Group: {spec},
	}, stdin, append([]string{spec.Group}, args...)...)
}

func runGeneratedRootCommand(t *testing.T, serverURL, apiKey string, groups map[string][]*command.Spec, args ...string) cmdResult {
	t.Helper()

	return runGeneratedRootCommandWithInput(t, serverURL, apiKey, groups, nil, args...)
}

func runGeneratedRootCommandWithInput(t *testing.T, serverURL, apiKey string, groups map[string][]*command.Spec, stdin io.Reader, args ...string) cmdResult {
	t.Helper()

	var stdout, stderr bytes.Buffer
	formatter := formatterForArgs(args, &stdout, &stderr)

	t.Setenv("HEYGEN_API_KEY", apiKey)
	t.Setenv("HEYGEN_API_BASE", serverURL)
	t.Setenv("HEYGEN_ALLOW_HTTP", "1")
	t.Setenv("HEYGEN_NO_ANALYTICS", "1")
	if _, ok := os.LookupEnv("HEYGEN_CONFIG_DIR"); !ok {
		t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	}

	root := newRootCmdWithSpecs("test", formatter, analytics.New("test", false), groups)
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(args)
	if stdin != nil {
		root.SetIn(stdin)
	}

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
