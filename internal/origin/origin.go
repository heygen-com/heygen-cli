// Package origin identifies the parent agent / IDE / managed sandbox that
// spawned this heygen-cli invocation, if any.
//
// The signal is propagated to the HeyGen API as an X-HeyGen-Client-Origin
// header (see internal/client) and attached to PostHog analytics events
// (see internal/analytics). Both surfaces are best-effort attribution —
// downstream code must tolerate the empty string for "unknown".
//
// What we read: well-known environment variable existence (we never read
// the *value* of an env var — some are API keys) plus filesystem markers
// for a small number of vendors that don't propagate a unique env var.
// What we never read: file contents (beyond known kernel/sandbox markers),
// process arguments, or any user data.
//
// Detection rules are ported from the hyperframes-oss CLI's
// agent_runtime.ts so the two surfaces stay in lockstep on which agent
// names exist and how each is identified.
package origin

import (
	"os"
	"runtime"
	"strings"
)

// Origin is the opaque identifier surfaced for the parent agent/IDE.
// Empty string means "no parent detected" — keep this stable since the
// backend keys on these strings to qualify the request-source label.
type Origin string

const (
	None               Origin = ""
	ClaudeCode         Origin = "claude_code"
	Codex              Origin = "codex"
	Cursor             Origin = "cursor"
	Windsurf           Origin = "windsurf"
	CopilotAgent       Origin = "copilot_agent"
	Replit             Origin = "replit"
	Hermes             Origin = "hermes"
	Openclaw           Origin = "openclaw"
	Pi                 Origin = "pi"
	Cline              Origin = "cline"
	GeminiCLI          Origin = "gemini_cli"
	Crush              Origin = "crush"
	GeminiManagedAgent Origin = "gemini_managed_agent"
)

// envChecker is a single rule. Order matters: first match wins, so place
// more specific rules before more generic ones.
type envChecker struct {
	name  Origin
	check func() bool
}

// envExists returns true iff the named env var is set to a non-empty string.
// We deliberately key on existence rather than value because some markers
// (e.g. GEMINI_API_KEY in similar detector packages) are secrets we must
// never log or compare against.
func envExists(key string) bool {
	return os.Getenv(key) != ""
}

// vendorRules is the ordered env-var rule set, ported from hyperframes-oss
// packages/cli/src/telemetry/agent_runtime.ts VENDOR_RULES.
//
// Each comment cites the source-of-truth in the agent's own code where the
// marker is set. If you add a vendor, follow the same bar: a marker that the
// agent provably sets in the spawned subprocess environment (not just a
// nominal Dockerfile ENV — empirical absence in the published runtime image
// is why OpenHands was rejected). Verify the rule before shipping.
var vendorRules = []envChecker{
	// Anthropic Claude Code — sets CLAUDECODE=1 on every Bash/PowerShell
	// tool spawn (Shell.ts:321) and CLAUDE_CODE_ENTRYPOINT at startup
	// (main.tsx:527). Both inherited by every child process.
	{ClaudeCode, func() bool { return envExists("CLAUDECODE") || envExists("CLAUDE_CODE_ENTRYPOINT") }},
	// OpenAI Codex. CODEX_THREAD_ID + CODEX_CI are set on every unified-exec
	// child (codex-rs/protocol/src/shell_environment.rs, process_manager.rs).
	// CODEX_SANDBOX_NETWORK_DISABLED is set when the network sandbox is
	// active (default-on). CODEX_HOME is intentionally NOT a marker — it's a
	// config override, not propagated to spawned children.
	{Codex, func() bool {
		return envExists("CODEX_THREAD_ID") || envExists("CODEX_CI") || envExists("CODEX_SANDBOX_NETWORK_DISABLED")
	}},
	// Cursor integrated terminal. Exact (case-sensitive) "cursor".
	{Cursor, func() bool { return os.Getenv("TERM_PROGRAM") == "cursor" }},
	// Windsurf (Codeium) integrated terminal. Case-INsensitive because
	// independent detectors disagree on casing ("windsurf" vs "Windsurf").
	// Marks the editor's integrated terminal, not specifically that the
	// Cascade agent is driving.
	{Windsurf, func() bool { return strings.ToLower(os.Getenv("TERM_PROGRAM")) == "windsurf" }},
	// GitHub Copilot Coding Agent — runs inside GitHub Actions. The
	// COPILOT_AGENT_ID + RUNNER_NAME markers should be verified against the
	// public docs before relying on attribution.
	{CopilotAgent, func() bool {
		return os.Getenv("GITHUB_ACTIONS") == "true" && (envExists("COPILOT_AGENT_ID") || os.Getenv("RUNNER_NAME") == "Copilot")
	}},
	// Replit workspace. REPL_ID + REPLIT_USER are long-documented
	// (https://docs.replit.com/replit-workspace/configuring-the-environment).
	{Replit, func() bool { return envExists("REPL_ID") || envExists("REPLIT_USER") }},
	// Nous Research Hermes Agent — cli.py:50 sets HERMES_QUIET="1"
	// unconditionally at module load (https://github.com/NousResearch/hermes-agent).
	{Hermes, func() bool { return envExists("HERMES_QUIET") }},
	// openclaw multi-channel AI gateway — propagates OPENCLAW_STATE_DIR /
	// OPENCLAW_CONFIG_PATH to every spawned CLI subprocess
	// (https://github.com/openclaw/openclaw).
	{Openclaw, func() bool { return envExists("OPENCLAW_STATE_DIR") || envExists("OPENCLAW_CONFIG_PATH") }},
	// Pi coding agent (https://pi.dev) — packages/coding-agent/src/cli.ts:13
	// sets PI_CODING_AGENT="true" at module entry.
	{Pi, func() bool { return envExists("PI_CODING_AGENT") }},
	// Cline VS Code extension — VscodeTerminalRegistry.ts:29 injects
	// CLINE_ACTIVE=true via the integrated-terminal env.
	{Cline, func() bool { return envExists("CLINE_ACTIVE") }},
	// Google Gemini CLI (open-source @google/gemini-cli) — distinct from
	// the Gemini managed-agent sandbox above. shellExecutionService.ts sets
	// GEMINI_CLI=1 on every shell command's child env.
	{GeminiCLI, func() bool { return envExists("GEMINI_CLI") }},
	// Crush (charmbracelet/crush) — internal/shell/shell.go appends CRUSH=1
	// to every shell it spawns. AGENT/AI_AGENT are too generic to key on.
	{Crush, func() bool { return envExists("CRUSH") }},
}

// Detect identifies the parent agent/IDE for this heygen-cli invocation,
// or returns None for a normal interactive shell. Cheap to call — the
// filesystem branch only runs on Linux and only stat-checks two paths.
//
// Caller convention: call once at startup, cache the result for the
// process lifetime (parent context doesn't change mid-invocation).
func Detect() Origin {
	// Filesystem-anchored detector ahead of the env-var loop: the Gemini
	// managed-agent sandbox is identified by a /.agents/ mount + gVisor
	// kernel pairing, not by env vars (GEMINI_API_KEY is user-settable on
	// any host).
	if isGeminiManagedAgent() {
		return GeminiManagedAgent
	}
	for _, rule := range vendorRules {
		if rule.check() {
			return rule.name
		}
	}
	return None
}

// isGeminiManagedAgent — Google Managed Agents platform. The runtime mounts
// /.agents/ for agent definitions (instructions + skills) and runs the
// sandbox under a gVisor kernel. We require BOTH signals to fire because
// neither alone is unique: nothing on gVisor (Cloud Run gen2, GKE Sandbox,
// Fly.io) mounts /.agents/ at the root, but a user could in principle
// create /.agents/ on any host; gVisor rules out that edge.
//
// We key on the DIRECTORY (not /.agents/AGENTS.md) since AGENTS.md is
// optional per Google's docs — an agent may declare its instructions
// inline via agent.yaml system_instruction.
func isGeminiManagedAgent() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	info, err := os.Stat("/.agents")
	if err != nil || !info.IsDir() {
		return false
	}
	return isGVisor()
}

// isGVisor — gVisor reports kernel string "4.19.0-gvisor" (current) or
// "4.4.0" (legacy Sentry). 4.19.0-gvisor is unambiguous; 4.4.0 collides
// with Ubuntu 16.04 LTS and is only accepted when /proc/version also
// contains the "gVisor" string.
func isGVisor() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	if uname, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
		if strings.Contains(string(uname), "gvisor") {
			return true
		}
	}
	procVersion, err := os.ReadFile("/proc/version")
	if err != nil {
		return false
	}
	return strings.Contains(string(procVersion), "gVisor")
}
