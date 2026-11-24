package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// assertUsageEnvelope checks stderr carries the canonical error envelope with
// the usage_error code, matching the repo convention of asserting envelope
// shape (not just exit code) on failures.
func assertUsageEnvelope(t *testing.T, stderr string) {
	t.Helper()
	var envelope map[string]map[string]any
	if err := json.Unmarshal([]byte(stderr), &envelope); err != nil {
		t.Fatalf("stderr is not valid JSON: %v\nstderr: %s", err, stderr)
	}
	if envelope["error"]["code"] != "usage_error" {
		t.Errorf("error.code = %v, want %q", envelope["error"]["code"], "usage_error")
	}
}

func TestFeedback_Valid(t *testing.T) {
	// No server and no API key: feedback is skipAuth, so it must succeed
	// without credentials. Analytics is disabled in runCommand, so the event
	// isn't sent — the command should report that rather than a false thanks.
	res := runCommand(t, "", "", "feedback", "--rating", "4", "--comment", "nice")
	if res.ExitCode != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", res.ExitCode, res.Stderr)
	}

	var got feedbackResponse
	if err := json.Unmarshal([]byte(res.Stdout), &got); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, res.Stdout)
	}
	if got.Rating != 4 {
		t.Fatalf("rating = %d, want 4", got.Rating)
	}
	if got.Comment != "nice" {
		t.Fatalf("comment = %q, want %q", got.Comment, "nice")
	}
	if !strings.Contains(got.Message, "Analytics is disabled") {
		t.Fatalf("message = %q, want it to report analytics disabled", got.Message)
	}
}

func TestFeedback_RatingOnly(t *testing.T) {
	res := runCommand(t, "", "", "feedback", "--rating", "5")
	if res.ExitCode != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", res.ExitCode, res.Stderr)
	}
	if !json.Valid([]byte(res.Stdout)) {
		t.Fatalf("stdout is not valid JSON: %s", res.Stdout)
	}
}

func TestFeedback_MissingRating(t *testing.T) {
	res := runCommand(t, "", "", "feedback")
	if res.ExitCode != 2 {
		t.Fatalf("exit = %d, want 2 (stdout: %s)", res.ExitCode, res.Stdout)
	}
	assertUsageEnvelope(t, res.Stderr)
}

func TestFeedback_CommentTooLong(t *testing.T) {
	long := strings.Repeat("x", 2001)
	res := runCommand(t, "", "", "feedback", "--rating", "3", "--comment", long)
	if res.ExitCode != 2 {
		t.Fatalf("exit = %d, want 2 (stdout: %s)", res.ExitCode, res.Stdout)
	}
	assertUsageEnvelope(t, res.Stderr)
}

func TestFeedback_CommentAtCap(t *testing.T) {
	atCap := strings.Repeat("x", 2000)
	res := runCommand(t, "", "", "feedback", "--rating", "3", "--comment", atCap)
	if res.ExitCode != 0 {
		t.Fatalf("exit = %d, want 0 (2000 chars is allowed) (stderr: %s)", res.ExitCode, res.Stderr)
	}
}

func TestFeedback_CommentCapCountsRunesNotBytes(t *testing.T) {
	// 2000 multibyte runes is 4000 bytes; it must pass because the cap is on
	// characters, not bytes.
	multibyte := strings.Repeat("é", 2000)
	res := runCommand(t, "", "", "feedback", "--rating", "3", "--comment", multibyte)
	if res.ExitCode != 0 {
		t.Fatalf("exit = %d, want 0 (2000 runes is allowed) (stderr: %s)", res.ExitCode, res.Stderr)
	}
}

func TestFeedback_RatingOutOfRange(t *testing.T) {
	for _, r := range []string{"0", "6", "9"} {
		res := runCommand(t, "", "", "feedback", "--rating", r)
		if res.ExitCode != 2 {
			t.Fatalf("rating %s: exit = %d, want 2 (stdout: %s)", r, res.ExitCode, res.Stdout)
		}
		assertUsageEnvelope(t, res.Stderr)
		if !strings.Contains(res.Stderr, "rating must be between 1 and 5") {
			t.Errorf("rating %s: stderr missing range message: %s", r, res.Stderr)
		}
	}
}

// The API's feedback endpoint generates a "feedback" group. It must sit under
// the hand-written command, not replace it: both invocations have to resolve.
func TestFeedback_GeneratedCreateSitsUnderHandWrittenCommand(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v3/feedback" {
			t.Errorf("request = %s %s, want POST /v3/feedback", r.Method, r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"id":"fb_1"}}`))
	}))
	defer srv.Close()

	res := runCommand(t, srv.URL, "test-key", "feedback", "create",
		"--resource-type", "video", "--resource-id", "vid_1", "--rating", "negative")
	if res.ExitCode != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", res.ExitCode, res.Stderr)
	}
	if gotBody["rating"] != "negative" || gotBody["resource_id"] != "vid_1" {
		t.Fatalf("body = %v, want rating=negative resource_id=vid_1", gotBody)
	}

	// The anonymous CLI rating still works with no key and no server.
	res = runCommand(t, "", "", "feedback", "--rating", "4")
	if res.ExitCode != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", res.ExitCode, res.Stderr)
	}
}

// The API command needs a key even though its hand-written parent does not.
func TestFeedback_GeneratedCreateRequiresCredentials(t *testing.T) {
	res := runCommand(t, "http://127.0.0.1:1", "", "feedback", "create",
		"--resource-type", "video", "--resource-id", "vid_1", "--rating", "negative")
	// Exit 3 with "no API key found" is the credential check itself. A command
	// that skipped it would fail later, on the request, with a different error.
	if res.ExitCode != 3 {
		t.Fatalf("exit = %d, want 3 (stderr: %s)", res.ExitCode, res.Stderr)
	}
	if !strings.Contains(res.Stderr, "no API key found") {
		t.Fatalf("stderr = %s, want the missing-key error", res.Stderr)
	}
}
