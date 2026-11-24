package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// videoCreateServer answers POST /v3/videos with statuses[i] for the i-th
// request and records each request body.
func videoCreateServer(t *testing.T, statuses ...int) (*httptest.Server, *[]map[string]any) {
	t.Helper()
	var bodies []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v3/videos" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		bodies = append(bodies, body)
		if len(bodies) > len(statuses) {
			t.Errorf("unexpected request %d: %v", len(bodies), body)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		status := statuses[len(bodies)-1]
		w.WriteHeader(status)
		if status >= 400 {
			_, _ = w.Write([]byte(`{"error":{"code":"resolution_not_allowed","message":"Your current plan does not support 1080p video."}}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"video_id":"video-1","status":"waiting"}}`))
	}))
	t.Cleanup(server.Close)
	return server, &bodies
}

func engineType(body map[string]any) any {
	engine, _ := body["engine"].(map[string]any)
	return engine["type"]
}

func TestVideoCreateDefaultEngine(t *testing.T) {
	cases := []struct {
		name string
		body string
		want any // engine type on the wire; nil means no engine key
	}{
		{"avatar", `{"type":"avatar","avatar_id":"a","script":"Hi","voice_id":"v"}`, defaultAvatarEngine},
		{"image", `{"type":"image","image":{"type":"url","url":"https://example.com/a.png"},"script":"Hi","voice_id":"v"}`, defaultAvatarEngine},
		{"1080p", `{"type":"avatar","avatar_id":"a","script":"Hi","voice_id":"v","resolution":"1080p"}`, defaultAvatarEngine},
		{"engine named", `{"type":"avatar","avatar_id":"a","script":"Hi","voice_id":"v","engine":{"type":"avatar_iv"}}`, "avatar_iv"},
		{"720p", `{"type":"avatar","avatar_id":"a","script":"Hi","voice_id":"v","resolution":"720p"}`, nil},
		{"4k", `{"type":"avatar","avatar_id":"a","script":"Hi","voice_id":"v","resolution":"4k"}`, nil},
		{"expressiveness", `{"type":"image","image":{"type":"url","url":"https://example.com/a.png"},"script":"Hi","voice_id":"v","expressiveness":"high"}`, nil},
		{"other type", `{"type":"studio","scenes":[]}`, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server, bodies := videoCreateServer(t, http.StatusOK)
			result := runCommand(t, server.URL, "test-key", "video", "create", "-d", tc.body)
			if result.ExitCode != 0 || result.Stderr != "" {
				t.Fatalf("exit=%d stderr=%s", result.ExitCode, result.Stderr)
			}
			if len(*bodies) != 1 {
				t.Fatalf("requests=%d", len(*bodies))
			}
			if got := engineType((*bodies)[0]); got != tc.want {
				t.Fatalf("engine=%v want %v", got, tc.want)
			}
		})
	}
}

func TestVideoCreateDefaultEngineRefusedSendsBodyAsWritten(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusPaymentRequired, http.StatusForbidden, http.StatusNotFound} {
		server, bodies := videoCreateServer(t, status, http.StatusOK)
		result := runCommand(t, server.URL, "test-key", "video", "create", "--idempotency-key", "k1",
			"-d", `{"type":"avatar","avatar_id":"a","script":"Hi","voice_id":"v"}`)
		wantWarning := "warning: " + defaultAvatarEngine + " is not available for this request; sent without a default engine\n"
		if result.ExitCode != 0 || result.Stderr != wantWarning {
			t.Fatalf("status %d: exit=%d stderr=%q", status, result.ExitCode, result.Stderr)
		}
		if len(*bodies) != 2 || engineType((*bodies)[0]) != defaultAvatarEngine {
			t.Fatalf("status %d: requests=%v", status, *bodies)
		}
		if _, set := (*bodies)[1]["engine"]; set || (*bodies)[1]["avatar_id"] != "a" {
			t.Fatalf("status %d: second request not as written: %v", status, (*bodies)[1])
		}
		if !strings.Contains(result.Stdout, `"video_id":"video-1"`) {
			t.Fatalf("status %d: stdout=%s", status, result.Stdout)
		}
	}
}

// When both attempts are refused the caller sees the answer to the body they
// wrote, not one about an engine they never asked for.
func TestVideoCreateDefaultEngineBothRefused(t *testing.T) {
	server, bodies := videoCreateServer(t, http.StatusBadRequest, http.StatusBadRequest)
	result := runCommand(t, server.URL, "test-key", "video", "create", "-d", `{"type":"avatar","avatar_id":"a","script":"Hi","voice_id":"v"}`)
	if result.ExitCode == 0 || len(*bodies) != 2 || !strings.Contains(result.Stderr, "resolution_not_allowed") {
		t.Fatalf("exit=%d requests=%d stderr=%s", result.ExitCode, len(*bodies), result.Stderr)
	}
}

func TestVideoCreateNoSecondAttempt(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
	}{
		{"server error", http.StatusInternalServerError, `{"type":"avatar","avatar_id":"a","script":"Hi","voice_id":"v"}`},
		{"unauthorized", http.StatusUnauthorized, `{"type":"avatar","avatar_id":"a","script":"Hi","voice_id":"v"}`},
		{"engine named", http.StatusBadRequest, `{"type":"avatar","avatar_id":"a","script":"Hi","voice_id":"v","engine":{"type":"heygen-avatar-6-turbo"}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server, bodies := videoCreateServer(t, tc.status)
			result := runCommand(t, server.URL, "test-key", "video", "create", "-d", tc.body)
			if result.ExitCode == 0 || len(*bodies) != 1 {
				t.Fatalf("exit=%d requests=%d", result.ExitCode, len(*bodies))
			}
		})
	}
}

func TestVideoBatchesCreateKeepsItemsAsWritten(t *testing.T) {
	server := setupTestServer(t, map[string]testHandler{
		"POST /v3/videos/batches": {
			StatusCode: http.StatusOK,
			Body:       `{"data":{"batch_id":"batch-1"}}`,
			ValidateRequest: func(t *testing.T, r *http.Request) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				if strings.Contains(strings.ToLower(string(mustJSON(t, body))), "engine") {
					t.Errorf("batch body changed: %v", body)
				}
			},
		},
	})
	defer server.Close()
	result := runCommand(t, server.URL, "test-key", "video", "batches", "create",
		"-d", `{"videos":[{"type":"avatar","avatar_id":"a","script":"Hi","voice_id":"v"}]}`)
	if result.ExitCode != 0 {
		t.Fatalf("exit=%d stderr=%s", result.ExitCode, result.Stderr)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestVideoCreateHelpStatesDefaultEngine(t *testing.T) {
	help := runCommand(t, "http://127.0.0.1:0", "test-key", "video", "create", "--help")
	if help.ExitCode != 0 {
		t.Fatalf("exit code = %d, stderr = %s", help.ExitCode, help.Stderr)
	}
	for _, want := range []string{
		"CLI default (replaces the API default above): when the request names no `engine`, the CLI sends `heygen-avatar-6-turbo`",
		"`expressiveness`, or a `resolution` other than 1080p",
		"resends the request as written",
	} {
		if !strings.Contains(help.Stdout, want) {
			t.Errorf("help is missing %q:\n%s", want, help.Stdout)
		}
	}

	other := runCommand(t, "http://127.0.0.1:0", "test-key", "video", "list", "--help")
	if strings.Contains(other.Stdout, "CLI default") {
		t.Errorf("video list help carries the video create note:\n%s", other.Stdout)
	}
}
