package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
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

	// Build a Cobra tree using the generic builder instead of hand-written newVideoListCmd
	ctx := &cmdContext{formatter: nil} // will be set by runGenCommand
	res := runGenCommand(t, srv.URL, "test-key", ctx, videoListSpec, "list")

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

	ctx := &cmdContext{}
	res := runGenCommand(t, srv.URL, "test-key", ctx, videoListSpec,
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

	ctx := &cmdContext{}
	res := runGenCommand(t, srv.URL, "test-key", ctx, spec,
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
			{Name: "video-id", Target: "path", Param: "video_id"},
		},
		Examples: []string{"heygen video get abc123"},
	}

	ctx := &cmdContext{}
	res := runGenCommand(t, srv.URL, "test-key", ctx, spec, "get", "abc123")

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

	ctx := &cmdContext{}
	res := runGenCommand(t, srv.URL, "test-key", ctx, spec, "create")

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

	ctx := &cmdContext{}
	res := runGenCommand(t, srv.URL, "test-key", ctx, spec, "list", "--type", "invalid")

	if res.ExitCode != 2 {
		t.Errorf("ExitCode = %d, want 2 (usage error)\nstderr: %s", res.ExitCode, res.Stderr)
	}
}

// runGenCommand builds a Cobra tree using the generic builder and executes it,
// similar to runCommand but using buildGenCommand instead of the hand-written commands.
//
// It creates a fresh root command (with PersistentPreRunE that sets up the client)
// and adds the spec-built command under a group. The cmdContext is shared between
// PersistentPreRunE (which sets ctx.client) and the spec command (which uses it).
func runGenCommand(t *testing.T, serverURL, apiKey string, _ *cmdContext, spec *command.Spec, args ...string) cmdResult {
	t.Helper()

	var stdout, stderr bytes.Buffer
	formatter := output.NewJSONFormatter(&stdout, &stderr)

	t.Setenv("HEYGEN_API_KEY", apiKey)
	t.Setenv("HEYGEN_API_BASE", serverURL)

	// newRootCmd creates a root with PersistentPreRunE that populates ctx.client.
	// We need the SAME ctx that PersistentPreRunE writes to.
	root := newRootCmd("test", formatter)

	// Extract the ctx from the root command by accessing the video group's closure.
	// Instead, we inject the spec command by replacing the video group.
	// The trick: newRootCmd already creates a cmdContext and shares it via closures.
	// We need to hook into that same context. The simplest way: add a custom
	// PersistentPreRunE wrapper that captures the context after the original runs.
	//
	// Actually, the cleanest approach: build a root that uses our spec.
	// We re-create the root with the spec command instead of the hand-written ones.
	root = newRootCmdWithSpecs("test", formatter, map[string][]*command.Spec{
		spec.Group: {spec},
	})

	fullArgs := append([]string{spec.Group}, args...)
	root.SetArgs(fullArgs)

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

