package analytics

import (
	"testing"
	"time"

	"github.com/posthog/posthog-go"
)

type stubCaptureClient struct {
	messages []posthog.Message
	closed   int
}

func (s *stubCaptureClient) Enqueue(msg posthog.Message) error {
	s.messages = append(s.messages, msg)
	return nil
}

func (s *stubCaptureClient) Close() error {
	s.closed++
	return nil
}

func TestCommandRun_Properties(t *testing.T) {
	stub := &stubCaptureClient{}
	client := newWithCapture("v1.2.3", stub)
	client.distinctID = "anon-id"

	client.CommandRun("heygen video list")

	if !client.Started() {
		t.Fatal("Started = false, want true")
	}
	if len(stub.messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(stub.messages))
	}

	msg, ok := stub.messages[0].(posthog.Capture)
	if !ok {
		t.Fatalf("message type = %T, want posthog.Capture", stub.messages[0])
	}
	if msg.DistinctId != "anon-id" {
		t.Fatalf("DistinctId = %q, want %q", msg.DistinctId, "anon-id")
	}
	if msg.Event != "COMMAND_RUN" {
		t.Fatalf("Event = %q, want %q", msg.Event, "COMMAND_RUN")
	}
	if got := msg.Properties["command"]; got != "heygen video list" {
		t.Fatalf("command = %v, want %q", got, "heygen video list")
	}
	if got := msg.Properties["cli_version"]; got != "v1.2.3" {
		t.Fatalf("cli_version = %v, want %q", got, "v1.2.3")
	}
}

func TestCommandRunComplete_Properties(t *testing.T) {
	stub := &stubCaptureClient{}
	client := newWithCapture("v1.2.3", stub)
	client.distinctID = "anon-id"

	client.CommandRunComplete("heygen video create", 4, 1500*time.Millisecond, "timeout", "cli", 0)

	if len(stub.messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(stub.messages))
	}

	msg, ok := stub.messages[0].(posthog.Capture)
	if !ok {
		t.Fatalf("message type = %T, want posthog.Capture", stub.messages[0])
	}
	if msg.Event != "COMMAND_RUN_COMPLETE" {
		t.Fatalf("Event = %q, want %q", msg.Event, "COMMAND_RUN_COMPLETE")
	}
	if got := msg.Properties["command"]; got != "heygen video create" {
		t.Fatalf("command = %v, want %q", got, "heygen video create")
	}
	if got := msg.Properties["exit_code"]; got != 4 {
		t.Fatalf("exit_code = %v, want %d", got, 4)
	}
	if got := msg.Properties["duration_ms"]; got != int64(1500) {
		t.Fatalf("duration_ms = %v, want %d", got, int64(1500))
	}
	if got := msg.Properties["success"]; got != false {
		t.Fatalf("success = %v, want false", got)
	}
	if got := msg.Properties["error_code"]; got != "timeout" {
		t.Fatalf("error_code = %v, want %q", got, "timeout")
	}
}

func TestCommandRun_IncludesClientOrigin(t *testing.T) {
	stub := &stubCaptureClient{}
	client := newWithCapture("v1.2.3", stub)
	client.clientOrigin = "cursor"

	client.CommandRun("heygen video list")

	msg := stub.messages[0].(posthog.Capture)
	if got := msg.Properties["client_origin"]; got != "cursor" {
		t.Fatalf("client_origin = %v, want %q", got, "cursor")
	}
}

func TestCommandRunComplete_IncludesClientOrigin(t *testing.T) {
	stub := &stubCaptureClient{}
	client := newWithCapture("v1.2.3", stub)
	client.clientOrigin = "claude_code"

	client.CommandRunComplete("heygen video create", 0, time.Second, "", "", 0)

	msg := stub.messages[0].(posthog.Capture)
	if got := msg.Properties["client_origin"]; got != "claude_code" {
		t.Fatalf("client_origin = %v, want %q", got, "claude_code")
	}
}

// Origin "" must NOT land in PostHog as `client_origin: ""` — that would
// pollute the property's value distribution (the empty bucket and the
// unknown bucket are different cohorts in funnel analysis).
func TestCommandRun_OmitsClientOriginWhenEmpty(t *testing.T) {
	stub := &stubCaptureClient{}
	client := newWithCapture("v1.2.3", stub)
	client.clientOrigin = ""

	client.CommandRun("heygen video list")

	msg := stub.messages[0].(posthog.Capture)
	if _, present := msg.Properties["client_origin"]; present {
		t.Fatalf("client_origin set to %v despite empty origin", msg.Properties["client_origin"])
	}
}

func TestFeedback_Properties(t *testing.T) {
	stub := &stubCaptureClient{}
	client := newWithCapture("v1.2.3", stub)
	client.distinctID = "anon-id"

	if !client.Feedback(5, "love it") {
		t.Fatal("Feedback returned false for an enabled client")
	}
	if len(stub.messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(stub.messages))
	}

	msg, ok := stub.messages[0].(posthog.Capture)
	if !ok {
		t.Fatalf("message type = %T, want posthog.Capture", stub.messages[0])
	}
	if msg.Event != "CLI_FEEDBACK" {
		t.Fatalf("Event = %q, want %q", msg.Event, "CLI_FEEDBACK")
	}
	if msg.DistinctId != "anon-id" {
		t.Fatalf("DistinctId = %q, want %q", msg.DistinctId, "anon-id")
	}
	if got := msg.Properties["rating"]; got != 5 {
		t.Fatalf("rating = %v, want 5", got)
	}
	if got := msg.Properties["comment"]; got != "love it" {
		t.Fatalf("comment = %v, want %q", got, "love it")
	}
	if got := msg.Properties["cli_version"]; got != "v1.2.3" {
		t.Fatalf("cli_version = %v, want %q", got, "v1.2.3")
	}
}

// An empty comment must not land as comment:"" — the absent and empty cohorts
// are distinct in analysis (same reasoning as client_origin).
func TestFeedback_OmitsCommentWhenEmpty(t *testing.T) {
	stub := &stubCaptureClient{}
	client := newWithCapture("v1.2.3", stub)

	client.Feedback(3, "")

	msg := stub.messages[0].(posthog.Capture)
	if _, present := msg.Properties["comment"]; present {
		t.Fatalf("comment present (%v) despite empty input", msg.Properties["comment"])
	}
}

// Anonymity guarantee: every event must carry $ip=null so PostHog neither
// stores nor geolocates the caller's IP.
func TestBaseProperties_SuppressesIP(t *testing.T) {
	stub := &stubCaptureClient{}
	client := newWithCapture("v1.2.3", stub)

	client.CommandRun("heygen video list")
	client.Feedback(5, "ok")

	for i, m := range stub.messages {
		props := m.(posthog.Capture).Properties
		ip, present := props["$ip"]
		if !present || ip != nil {
			t.Fatalf("message %d: $ip = %v (present=%v), want present and nil", i, ip, present)
		}
	}
}

func TestFeedback_DisabledReturnsFalse(t *testing.T) {
	client := New("test", false)
	if client.Feedback(5, "x") {
		t.Fatal("Feedback returned true for a disabled client")
	}
}

func TestClose_DisabledNoop(t *testing.T) {
	client := New("test", false)
	client.Close()

	if client.Started() {
		t.Fatal("Started = true, want false")
	}
}

func TestDistinctID_Persists(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())

	first := distinctID()
	second := distinctID()

	if first == "" {
		t.Fatal("first distinct ID is empty")
	}
	if second != first {
		t.Fatalf("second distinct ID = %q, want %q", second, first)
	}
}

func TestCommandRunComplete_SourceAndHTTPStatus(t *testing.T) {
	t.Run("api source carries http_status", func(t *testing.T) {
		stub := &stubCaptureClient{}
		client := newWithCapture("v1.2.3", stub)
		client.CommandRunComplete("heygen video create", 1, time.Second, "insufficient_credit", "api", 402)
		msg := stub.messages[0].(posthog.Capture)
		if got := msg.Properties["source"]; got != "api" {
			t.Fatalf("source = %v, want api", got)
		}
		if got := msg.Properties["http_status"]; got != 402 {
			t.Fatalf("http_status = %v, want 402", got)
		}
	})
	t.Run("cli source omits http_status when 0", func(t *testing.T) {
		stub := &stubCaptureClient{}
		client := newWithCapture("v1.2.3", stub)
		client.CommandRunComplete("heygen video create", 1, time.Second, "cli_file_io_error", "cli", 0)
		msg := stub.messages[0].(posthog.Capture)
		if got := msg.Properties["source"]; got != "cli" {
			t.Fatalf("source = %v, want cli", got)
		}
		if _, ok := msg.Properties["http_status"]; ok {
			t.Fatalf("http_status should be omitted when 0")
		}
	})
	t.Run("success omits source", func(t *testing.T) {
		stub := &stubCaptureClient{}
		client := newWithCapture("v1.2.3", stub)
		client.CommandRunComplete("heygen video list", 0, time.Second, "", "", 0)
		msg := stub.messages[0].(posthog.Capture)
		if _, ok := msg.Properties["source"]; ok {
			t.Fatalf("source should be omitted on success")
		}
	})
}
