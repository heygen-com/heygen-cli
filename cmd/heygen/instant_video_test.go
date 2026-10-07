package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestInstantVideoCreateModes(t *testing.T) {
	for _, mode := range []string{"text_to_video", "image_to_video", "reference_to_video"} {
		t.Run(mode, func(t *testing.T) {
			body := map[string]any{"model": "heygen-video-1", "mode": mode, "prompt": "A person waves", "duration": 5}
			if mode == "image_to_video" {
				body["image"] = map[string]string{"type": "asset_id", "asset_id": "asset-1"}
			}
			if mode == "reference_to_video" {
				body["reference_images"] = []map[string]string{{"type": "asset_id", "asset_id": "asset-1"}}
			}
			encoded, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			requests := 0
			server := setupTestServer(t, map[string]testHandler{
				"POST /v3/models/videos": {
					StatusCode: http.StatusAccepted,
					Body:       `{"data":{"video_id":"video-1","status":"pending"}}`,
					ValidateRequest: func(t *testing.T, r *http.Request) {
						requests++
						if r.Header.Get("X-Api-Key") != "test-key" {
							t.Error("missing API key")
						}
						if r.Header.Get("Idempotency-Key") != "generation-1" {
							t.Error("missing idempotency key")
						}
						var got map[string]any
						if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
							t.Fatal(err)
						}
						if got["mode"] != mode || got["model"] != "heygen-video-1" || got["prompt"] != "A person waves" {
							t.Fatalf("unexpected request: %#v", got)
						}
						if mode == "image_to_video" && got["image"] == nil {
							t.Error("first frame missing")
						}
						if mode == "reference_to_video" && got["reference_images"] == nil {
							t.Error("references missing")
						}
					},
				},
			})
			defer server.Close()
			result := runCommand(t, server.URL, "test-key", "model", "videos", "create", "--idempotency-key", "generation-1", "-d", string(encoded))
			if result.ExitCode != 0 || result.Stderr != "" {
				t.Fatalf("exit=%d stderr=%s", result.ExitCode, result.Stderr)
			}
			if requests != 1 || !strings.Contains(result.Stdout, `"video_id":"video-1"`) {
				t.Fatalf("requests=%d stdout=%s", requests, result.Stdout)
			}
		})
	}
}

func TestInstantVideoGet(t *testing.T) {
	server := setupTestServer(t, map[string]testHandler{
		"GET /v3/models/videos/video-1": {
			StatusCode: http.StatusOK,
			Body:       `{"data":{"video_id":"video-1","status":"completed","video_url":"https://example.com/result.mp4"}}`,
		},
	})
	defer server.Close()
	result := runCommand(t, server.URL, "test-key", "model", "videos", "get", "video-1")
	if result.ExitCode != 0 || result.Stderr != "" {
		t.Fatalf("exit=%d stderr=%s", result.ExitCode, result.Stderr)
	}
	if !strings.Contains(result.Stdout, "https://example.com/result.mp4") {
		t.Fatalf("missing result URL: %s", result.Stdout)
	}
}
