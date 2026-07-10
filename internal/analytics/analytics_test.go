package analytics

import (
	"encoding/json"
	"os"
	"path/filepath"
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
	if got := msg.Properties["surface"]; got != "heygen-cli" {
		t.Fatalf("surface = %v, want %q", got, "heygen-cli")
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
	if got := msg.Properties["surface"]; got != "heygen-cli" {
		t.Fatalf("surface = %v, want %q", got, "heygen-cli")
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

// distinctID now reads/writes ~/.hyperframes/config.json (resolved via
// os.UserHomeDir, i.e. $HOME), so every test below isolates both HOME and
// HEYGEN_CONFIG_DIR — never touch a developer's real shared config file.

func TestDistinctID_Persists(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
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

// U1: shared config file exists with an anonymousId → that value is used
// verbatim.
func TestDistinctID_UsesSharedConfigAnonymousId(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())

	hfDir := filepath.Join(home, ".hyperframes")
	if err := os.MkdirAll(hfDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(hfDir, "config.json"), []byte(`{"anonymousId":"shared-id-123"}`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if got := distinctID(); got != "shared-id-123" {
		t.Fatalf("distinctID() = %q, want %q", got, "shared-id-123")
	}
}

// U1: shared config absent, legacy ~/.heygen/analytics_id exists → its
// value is promoted into a newly written shared config file and used.
func TestDistinctID_PromotesLegacyId(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configDir := t.TempDir()
	t.Setenv("HEYGEN_CONFIG_DIR", configDir)

	if err := os.WriteFile(filepath.Join(configDir, "analytics_id"), []byte("legacy-id-456\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got := distinctID()
	if got != "legacy-id-456" {
		t.Fatalf("distinctID() = %q, want %q", got, "legacy-id-456")
	}

	raw, err := os.ReadFile(filepath.Join(home, ".hyperframes", "config.json"))
	if err != nil {
		t.Fatalf("ReadFile shared config: %v", err)
	}
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatalf("Unmarshal shared config: %v", err)
	}
	if config["anonymousId"] != "legacy-id-456" {
		t.Fatalf("shared config anonymousId = %v, want %q", config["anonymousId"], "legacy-id-456")
	}
}

// U1: neither the shared config nor a legacy id exists → a fresh id is
// minted and written to the shared config file.
func TestDistinctID_MintsFreshId(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())

	got := distinctID()
	if got == "" {
		t.Fatal("distinctID() is empty")
	}

	raw, err := os.ReadFile(filepath.Join(home, ".hyperframes", "config.json"))
	if err != nil {
		t.Fatalf("ReadFile shared config: %v", err)
	}
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatalf("Unmarshal shared config: %v", err)
	}
	if config["anonymousId"] != got {
		t.Fatalf("shared config anonymousId = %v, want %q", config["anonymousId"], got)
	}
}

// U1: a malformed shared config file must be treated as absent (falls
// through to the legacy-then-mint path) and must never panic.
func TestDistinctID_MalformedSharedConfigTreatedAsAbsent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configDir := t.TempDir()
	t.Setenv("HEYGEN_CONFIG_DIR", configDir)

	hfDir := filepath.Join(home, ".hyperframes")
	if err := os.MkdirAll(hfDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(hfDir, "config.json"), []byte("{not valid json"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "analytics_id"), []byte("legacy-from-malformed\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got := distinctID()
	if got != "legacy-from-malformed" {
		t.Fatalf("distinctID() = %q, want %q (malformed config should fall through)", got, "legacy-from-malformed")
	}
}

// U1: HEYGEN_CONFIG_DIR only affects the legacy-id read; the shared config
// path resolution stays anchored to HOME regardless.
func TestDistinctID_HeygenConfigDirIndependentOfSharedPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configDir := t.TempDir()
	t.Setenv("HEYGEN_CONFIG_DIR", configDir)

	if err := os.WriteFile(filepath.Join(configDir, "analytics_id"), []byte("from-override-dir\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got := distinctID()
	if got != "from-override-dir" {
		t.Fatalf("distinctID() = %q, want %q", got, "from-override-dir")
	}
	if _, err := os.Stat(filepath.Join(home, ".hyperframes", "config.json")); err != nil {
		t.Fatalf("shared config not written under HOME: %v", err)
	}
	if _, err := os.Stat(filepath.Join(configDir, "config.json")); err == nil {
		t.Fatal("shared config incorrectly written under HEYGEN_CONFIG_DIR")
	}
}

// U1: unrelated keys already in the shared config (written by hyperframes
// CLI or media-use) must survive a heygen-cli write untouched — proves
// read-merge-write, not a blind overwrite.
func TestDistinctID_PreservesUnrelatedSharedConfigKeys(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())

	hfDir := filepath.Join(home, ".hyperframes")
	if err := os.MkdirAll(hfDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(hfDir, "config.json"), []byte(`{"telemetryNoticeShown":true}`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got := distinctID()
	if got == "" {
		t.Fatal("distinctID() is empty")
	}

	raw, err := os.ReadFile(filepath.Join(hfDir, "config.json"))
	if err != nil {
		t.Fatalf("ReadFile shared config: %v", err)
	}
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatalf("Unmarshal shared config: %v", err)
	}
	if config["telemetryNoticeShown"] != true {
		t.Fatalf("telemetryNoticeShown = %v, want true (must survive heygen-cli's write)", config["telemetryNoticeShown"])
	}
	if config["anonymousId"] != got {
		t.Fatalf("anonymousId = %v, want %q", config["anonymousId"], got)
	}
}

// IdentifyAccount (U3): enqueues Alias then Identify, exactly once per
// process, no-ops on an empty distinct id or a disabled client.

func TestIdentifyAccount_EnqueuesAliasThenIdentify(t *testing.T) {
	stub := &stubCaptureClient{}
	client := newWithCapture("v1.2.3", stub)
	client.distinctID = "anon-id"

	client.IdentifyAccount("person@example.com")

	if len(stub.messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(stub.messages))
	}
	alias, ok := stub.messages[0].(posthog.Alias)
	if !ok {
		t.Fatalf("message 0 type = %T, want posthog.Alias", stub.messages[0])
	}
	if alias.DistinctId != "person@example.com" {
		t.Fatalf("Alias.DistinctId = %q, want %q", alias.DistinctId, "person@example.com")
	}
	if alias.Alias != "anon-id" {
		t.Fatalf("Alias.Alias = %q, want %q", alias.Alias, "anon-id")
	}
	identify, ok := stub.messages[1].(posthog.Identify)
	if !ok {
		t.Fatalf("message 1 type = %T, want posthog.Identify", stub.messages[1])
	}
	if identify.DistinctId != "person@example.com" {
		t.Fatalf("Identify.DistinctId = %q, want %q", identify.DistinctId, "person@example.com")
	}
}

func TestIdentifyAccount_UsernameFallback(t *testing.T) {
	stub := &stubCaptureClient{}
	client := newWithCapture("v1.2.3", stub)
	client.distinctID = "anon-id"

	client.IdentifyAccount("someusername")

	alias := stub.messages[0].(posthog.Alias)
	if alias.DistinctId != "someusername" || alias.Alias != "anon-id" {
		t.Fatalf("Alias = %+v, want DistinctId=someusername Alias=anon-id", alias)
	}
	identify := stub.messages[1].(posthog.Identify)
	if identify.DistinctId != "someusername" {
		t.Fatalf("Identify.DistinctId = %q, want %q", identify.DistinctId, "someusername")
	}
}

func TestIdentifyAccount_FiresOnlyOncePerProcess(t *testing.T) {
	stub := &stubCaptureClient{}
	client := newWithCapture("v1.2.3", stub)

	client.IdentifyAccount("first@example.com")
	client.IdentifyAccount("second@example.com")

	if len(stub.messages) != 2 {
		t.Fatalf("messages = %d, want 2 (second call must no-op)", len(stub.messages))
	}
}

func TestIdentifyAccount_EmptyDistinctIdNoops(t *testing.T) {
	stub := &stubCaptureClient{}
	client := newWithCapture("v1.2.3", stub)

	client.IdentifyAccount("")

	if len(stub.messages) != 0 {
		t.Fatalf("messages = %d, want 0 for an empty distinct id", len(stub.messages))
	}
}

func TestIdentifyAccount_DisabledClientNoops(t *testing.T) {
	client := New("test", false)
	// Must not panic on a nil capture client.
	client.IdentifyAccount("person@example.com")
}

// AuthLoginStarted/Completed/Failed (U4): each fires a single Capture
// event carrying method (and reason, for Failed).

func TestAuthLoginStarted_Properties(t *testing.T) {
	stub := &stubCaptureClient{}
	client := newWithCapture("v1.2.3", stub)
	client.distinctID = "anon-id"

	client.AuthLoginStarted("oauth")

	if len(stub.messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(stub.messages))
	}
	msg, ok := stub.messages[0].(posthog.Capture)
	if !ok {
		t.Fatalf("message type = %T, want posthog.Capture", stub.messages[0])
	}
	if msg.Event != "AUTH_LOGIN_STARTED" {
		t.Fatalf("Event = %q, want %q", msg.Event, "AUTH_LOGIN_STARTED")
	}
	if got := msg.Properties["method"]; got != "oauth" {
		t.Fatalf("method = %v, want %q", got, "oauth")
	}
	if got := msg.Properties["surface"]; got != "heygen-cli" {
		t.Fatalf("surface = %v, want %q", got, "heygen-cli")
	}
}

func TestAuthLoginCompleted_Properties(t *testing.T) {
	stub := &stubCaptureClient{}
	client := newWithCapture("v1.2.3", stub)

	client.AuthLoginCompleted("api_key")

	msg, ok := stub.messages[0].(posthog.Capture)
	if !ok {
		t.Fatalf("message type = %T, want posthog.Capture", stub.messages[0])
	}
	if msg.Event != "AUTH_LOGIN_COMPLETED" {
		t.Fatalf("Event = %q, want %q", msg.Event, "AUTH_LOGIN_COMPLETED")
	}
	if got := msg.Properties["method"]; got != "api_key" {
		t.Fatalf("method = %v, want %q", got, "api_key")
	}
}

func TestAuthLoginFailed_Properties(t *testing.T) {
	stub := &stubCaptureClient{}
	client := newWithCapture("v1.2.3", stub)

	client.AuthLoginFailed("oauth", "oauth_timeout")

	msg, ok := stub.messages[0].(posthog.Capture)
	if !ok {
		t.Fatalf("message type = %T, want posthog.Capture", stub.messages[0])
	}
	if msg.Event != "AUTH_LOGIN_FAILED" {
		t.Fatalf("Event = %q, want %q", msg.Event, "AUTH_LOGIN_FAILED")
	}
	if got := msg.Properties["method"]; got != "oauth" {
		t.Fatalf("method = %v, want %q", got, "oauth")
	}
	if got := msg.Properties["reason"]; got != "oauth_timeout" {
		t.Fatalf("reason = %v, want %q", got, "oauth_timeout")
	}
}

func TestAuthLoginEvents_DisabledClientNoop(t *testing.T) {
	client := New("test", false)
	// Must not panic on a nil capture client.
	client.AuthLoginStarted("oauth")
	client.AuthLoginCompleted("oauth")
	client.AuthLoginFailed("oauth", "oauth_timeout")
}
