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

	client.CommandRunComplete("heygen video create", 4, 1500*time.Millisecond, "timeout")

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
