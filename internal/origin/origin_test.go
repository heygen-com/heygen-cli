package origin

import (
	"os"
	"runtime"
	"testing"
)

// envKeysToClear lists every variable any vendor rule reads. We blank all of
// them around each subtest so the host shell's environment can't make a
// rule fire that the test didn't ask for (e.g. a developer running tests
// from inside a Cursor terminal would otherwise see Cursor "leak" into an
// unrelated subtest).
var envKeysToClear = []string{
	"CLAUDECODE",
	"CLAUDE_CODE_ENTRYPOINT",
	"CODEX_THREAD_ID",
	"CODEX_CI",
	"CODEX_SANDBOX_NETWORK_DISABLED",
	"TERM_PROGRAM",
	"GITHUB_ACTIONS",
	"COPILOT_AGENT_ID",
	"RUNNER_NAME",
	"REPL_ID",
	"REPLIT_USER",
	"HERMES_QUIET",
	"OPENCLAW_STATE_DIR",
	"OPENCLAW_CONFIG_PATH",
	"PI_CODING_AGENT",
	"CLINE_ACTIVE",
	"GEMINI_CLI",
	"CRUSH",
}

func withCleanEnv(t *testing.T, set map[string]string) {
	t.Helper()
	for _, k := range envKeysToClear {
		t.Setenv(k, "")
	}
	for k, v := range set {
		t.Setenv(k, v)
	}
}

func TestDetect_NoSignals(t *testing.T) {
	withCleanEnv(t, nil)
	if got := Detect(); got != None {
		t.Fatalf("Detect() = %q, want %q (no env signals)", got, None)
	}
}

func TestDetect_VendorRules(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want Origin
	}{
		// Claude Code — both markers fire the rule independently.
		{"claude_code via CLAUDECODE", map[string]string{"CLAUDECODE": "1"}, ClaudeCode},
		{"claude_code via CLAUDE_CODE_ENTRYPOINT", map[string]string{"CLAUDE_CODE_ENTRYPOINT": "cli"}, ClaudeCode},

		// Codex — three markers, all fire the rule independently.
		{"codex via CODEX_THREAD_ID", map[string]string{"CODEX_THREAD_ID": "abc"}, Codex},
		{"codex via CODEX_CI", map[string]string{"CODEX_CI": "1"}, Codex},
		{"codex via CODEX_SANDBOX_NETWORK_DISABLED", map[string]string{"CODEX_SANDBOX_NETWORK_DISABLED": "1"}, Codex},

		// Cursor — case-sensitive match on TERM_PROGRAM.
		{"cursor exact", map[string]string{"TERM_PROGRAM": "cursor"}, Cursor},
		// Case-mismatch regression: "Cursor" must NOT match the Cursor rule.
		// It does, however, satisfy the Windsurf rule's lowercase comparison
		// — oh wait, no, "Cursor"→"cursor" ≠ "windsurf". So this returns None.
		{"cursor wrong case → None", map[string]string{"TERM_PROGRAM": "Cursor"}, None},

		// Windsurf — case-insensitive match on TERM_PROGRAM.
		{"windsurf lowercase", map[string]string{"TERM_PROGRAM": "windsurf"}, Windsurf},
		{"windsurf titlecase", map[string]string{"TERM_PROGRAM": "Windsurf"}, Windsurf},
		{"windsurf uppercase", map[string]string{"TERM_PROGRAM": "WINDSURF"}, Windsurf},

		// Copilot — needs both GITHUB_ACTIONS=true AND a Copilot marker.
		{"copilot_agent via COPILOT_AGENT_ID", map[string]string{"GITHUB_ACTIONS": "true", "COPILOT_AGENT_ID": "x"}, CopilotAgent},
		{"copilot_agent via RUNNER_NAME=Copilot", map[string]string{"GITHUB_ACTIONS": "true", "RUNNER_NAME": "Copilot"}, CopilotAgent},
		// Without GITHUB_ACTIONS the rule must NOT fire — a hand-set
		// COPILOT_AGENT_ID outside Actions is not the Copilot agent runner.
		{"copilot_agent without GITHUB_ACTIONS → None", map[string]string{"COPILOT_AGENT_ID": "x"}, None},
		// GITHUB_ACTIONS alone with a non-Copilot runner is just a normal CI job.
		{"plain github actions → None", map[string]string{"GITHUB_ACTIONS": "true", "RUNNER_NAME": "ubuntu-22.04"}, None},

		// Remaining single-marker rules.
		{"replit via REPL_ID", map[string]string{"REPL_ID": "x"}, Replit},
		{"replit via REPLIT_USER", map[string]string{"REPLIT_USER": "x"}, Replit},
		{"hermes via HERMES_QUIET", map[string]string{"HERMES_QUIET": "1"}, Hermes},
		{"openclaw via OPENCLAW_STATE_DIR", map[string]string{"OPENCLAW_STATE_DIR": "/x"}, Openclaw},
		{"openclaw via OPENCLAW_CONFIG_PATH", map[string]string{"OPENCLAW_CONFIG_PATH": "/x"}, Openclaw},
		{"pi via PI_CODING_AGENT", map[string]string{"PI_CODING_AGENT": "1"}, Pi},
		{"cline via CLINE_ACTIVE", map[string]string{"CLINE_ACTIVE": "true"}, Cline},
		{"gemini_cli via GEMINI_CLI", map[string]string{"GEMINI_CLI": "1"}, GeminiCLI},
		{"crush via CRUSH", map[string]string{"CRUSH": "1"}, Crush},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withCleanEnv(t, tc.env)
			if got := Detect(); got != tc.want {
				t.Fatalf("Detect() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestDetect_FirstMatchWins anchors the ordering invariant in vendorRules.
// Claude Code is listed first, so when both CLAUDECODE and a Cursor-style
// TERM_PROGRAM are set, the answer must be claude_code — not Cursor — and
// must not depend on Go's map-iteration order.
func TestDetect_FirstMatchWins(t *testing.T) {
	withCleanEnv(t, map[string]string{
		"CLAUDECODE":   "1",
		"TERM_PROGRAM": "cursor",
	})
	if got := Detect(); got != ClaudeCode {
		t.Fatalf("Detect() = %q, want %q (claude_code outranks cursor in vendorRules)", got, ClaudeCode)
	}
}

func TestDetect_EmptyEnvVarIsNotSet(t *testing.T) {
	// `CLAUDECODE=""` is how shells unset a var via os.Setenv. envExists must
	// treat that as "not set" — otherwise every host with empty inherited
	// shell vars would report claude_code.
	withCleanEnv(t, map[string]string{"CLAUDECODE": ""})
	if got := Detect(); got != None {
		t.Fatalf("Detect() = %q, want %q (empty string must be treated as unset)", got, None)
	}
}

// TestIsGeminiManagedAgent_NonLinuxShortCircuits — without this short-circuit
// the macOS/Windows code path would hit os.ReadFile("/proc/version"), which
// is fine (returns an error) but it's worth pinning the early return.
func TestIsGeminiManagedAgent_NonLinuxShortCircuits(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("only meaningful on non-linux")
	}
	if isGeminiManagedAgent() {
		t.Fatal("isGeminiManagedAgent() = true on non-linux, want false")
	}
}

// TestIsGVisor_OnRealHost is a sanity guard: the test host is the dev
// box, which is NOT gVisor. If this ever returns true on a non-gVisor
// host the detector is over-eager and would mislabel real CI runs.
func TestIsGVisor_OnRealHost(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("only runs on linux")
	}
	// If we ARE on a gVisor host (very unlikely in CI), skip — the test is
	// guarding the false-positive direction.
	if data, err := os.ReadFile("/proc/version"); err == nil {
		if contains(data, "gVisor") {
			t.Skip("running on gVisor; test guards the non-gVisor case")
		}
	}
	if isGVisor() {
		t.Fatal("isGVisor() = true on non-gVisor host")
	}
}

func contains(haystack []byte, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if string(haystack[i:i+len(needle)]) == needle {
			return true
		}
	}
	return false
}
