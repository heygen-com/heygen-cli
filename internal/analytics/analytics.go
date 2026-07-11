package analytics

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/heygen-com/heygen-cli/internal/origin"
	"github.com/heygen-com/heygen-cli/internal/paths"
	"github.com/posthog/posthog-go"
)

// Same public ingestion key and project hyperframes-oss (packages/cli,
// skills/media-use) already ships to, so heygen-cli's events land in the
// same PostHog project and become queryable against the shared install
// identity (see distinctID / sharedConfigPath below).
const posthogAPIKey = "phc_zjjbX0PnWxERXrMHhkEJWj9A9BhGVLRReICgsfTMmpx"
const posthogEndpoint = "https://us.i.posthog.com"

// legacyPosthogAPIKey is heygen-cli's own pre-existing PostHog project,
// predating this identity/destination unification. Every event is
// dual-written here too so its existing dashboard keeps receiving fresh
// data (rather than flatlining as clients upgrade) until it's migrated to
// query the shared project instead.
const legacyPosthogAPIKey = "phc_Bvsz7U8BvVxZtguuTGiBcbRUCkX46MwqdZ9t8LQ7cBGr"

type captureClient interface {
	Enqueue(posthog.Message) error
	Close() error
}

// multiClient fans every Enqueue/Close out to every wrapped client, so a
// single call site (CommandRun, IdentifyAccount, etc.) can dual-write to
// more than one PostHog destination without any event-emitting method
// knowing about it. Continues past a failing client so one destination
// being down never blocks the others.
type multiClient struct {
	clients []captureClient
}

func (m *multiClient) Enqueue(msg posthog.Message) error {
	var firstErr error
	for _, c := range m.clients {
		if err := c.Enqueue(msg); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (m *multiClient) Close() error {
	var firstErr error
	for _, c := range m.clients {
		if err := c.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Client wraps PostHog event capture behind a small no-op-friendly surface.
type Client struct {
	ph           captureClient
	enabled      bool
	distinctID   string
	version      string
	clientOrigin string
	started      bool
	identified   bool
}

// New creates an analytics client. Disabled clients are inert no-ops.
func New(version string, enabled bool) *Client {
	if !enabled || posthogAPIKey == "" {
		return &Client{}
	}

	var clients []captureClient
	for _, key := range [...]string{posthogAPIKey, legacyPosthogAPIKey} {
		if key == "" {
			continue
		}
		ph, err := posthog.NewWithConfig(key, posthog.Config{
			BatchSize: 1,
			Endpoint:  posthogEndpoint,
		})
		if err == nil {
			clients = append(clients, ph)
		}
	}
	if len(clients) == 0 {
		return &Client{}
	}

	return newWithCapture(version, &multiClient{clients: clients})
}

func newWithCapture(version string, ph captureClient) *Client {
	return &Client{
		ph:           ph,
		enabled:      true,
		distinctID:   distinctID(),
		version:      version,
		clientOrigin: string(origin.Detect()),
	}
}

func (c *Client) CommandRun(command string) {
	if !c.enabled || c.ph == nil {
		return
	}
	c.started = true
	_ = c.ph.Enqueue(posthog.Capture{
		DistinctId: c.distinctID,
		Event:      "COMMAND_RUN",
		Properties: c.baseProperties(command),
	})
}

// CommandRunComplete records a completed command. On error, source is "api" (the
// error came from an API response envelope) or "cli" (CLI-generated), and httpStatus
// is the upstream status when known (0 otherwise); both are omitted on success.
func (c *Client) CommandRunComplete(command string, exitCode int, duration time.Duration, errorCode, source string, httpStatus int) {
	if !c.enabled || c.ph == nil {
		return
	}
	props := c.baseProperties(command).
		Set("exit_code", exitCode).
		Set("duration_ms", duration.Milliseconds()).
		Set("success", exitCode == 0).
		Set("error_code", errorCode)
	if source != "" {
		props = props.Set("source", source)
	}
	if httpStatus > 0 {
		props = props.Set("http_status", httpStatus)
	}
	_ = c.ph.Enqueue(posthog.Capture{
		DistinctId: c.distinctID,
		Event:      "COMMAND_RUN_COMPLETE",
		Properties: props,
	})
}

// Feedback records a satisfaction rating (1-5) with an optional free-text
// comment as a CLI_FEEDBACK event. Returns false when analytics is disabled
// (opt-out), so the caller can tell the user nothing was sent.
func (c *Client) Feedback(rating int, comment string) bool {
	if !c.enabled || c.ph == nil {
		return false
	}
	props := c.baseProperties("feedback").Set("rating", rating)
	// Omit an empty comment rather than sending "" — an empty value and an
	// absent one are different cohorts in analysis (cf. client_origin).
	if comment != "" {
		props = props.Set("comment", comment)
	}
	_ = c.ph.Enqueue(posthog.Capture{
		DistinctId: c.distinctID,
		Event:      "CLI_FEEDBACK",
		Properties: props,
	})
	return true
}

// baseProperties is the per-event property bundle every CLI event carries.
// Kept in one place so client_origin / cli_version / os / arch can't drift
// between COMMAND_RUN and COMMAND_RUN_COMPLETE — funnel queries break when
// the start and complete events disagree on dimensions.
func (c *Client) baseProperties(command string) posthog.Properties {
	props := posthog.NewProperties().
		Set("command", command).
		Set("cli_version", c.version).
		Set("os", runtime.GOOS).
		Set("arch", runtime.GOARCH).
		// heygen-cli's events now land in the same PostHog project as
		// hyperframes-oss and media-use (shared posthogAPIKey above); every
		// event needs this marker so a dashboard/query can isolate one
		// tool's traffic instead of relying only on event-name prefixes.
		Set("surface", "heygen-cli").
		// Send $ip: null so PostHog neither stores the caller's IP nor geolocates
		// it. The CLI is opt-in anonymous telemetry; IP would undermine that. PostHog
		// otherwise derives IP from the ingest request, so this must be set explicitly.
		Set("$ip", nil)
	if c.clientOrigin != "" {
		props = props.Set("client_origin", c.clientOrigin)
	}
	return props
}

func (c *Client) Close() {
	if c.enabled && c.ph != nil {
		_ = c.ph.Close()
	}
}

func (c *Client) Started() bool {
	return c.started
}

// IdentifyAccount links a resolved HeyGen account identity (email, falling
// back to username — the caller's job to pick) to this install's shared
// anonymous id, exactly once per process. This is the anon-to-identified
// merge: posthog-go v1.11.2's Identify message has no $anon_distinct_id
// field (Identify.Properties only becomes person-property $set data), so
// the actual merge primitive in this SDK version is the separate Alias
// message, which fires PostHog's $create_alias event. Direction matters —
// DistinctId is the identity being merged INTO (the account), Alias is the
// anonymous id being merged FROM; reversed, the merge silently fails.
func (c *Client) IdentifyAccount(distinctId string) {
	if !c.enabled || c.ph == nil || c.identified || distinctId == "" {
		return
	}
	c.identified = true
	_ = c.ph.Enqueue(posthog.Alias{
		DistinctId: distinctId,
		Alias:      c.distinctID,
	})
	_ = c.ph.Enqueue(posthog.Identify{
		DistinctId: distinctId,
	})
}

// AuthLoginStarted records that a login attempt was dispatched to a
// specific method ("oauth" / "api_key" / "device_code"), after the picker
// or an explicit flag resolved which one — never before a picker
// cancellation, so started reconciles cleanly to completed/failed.
func (c *Client) AuthLoginStarted(method string) {
	if !c.enabled || c.ph == nil {
		return
	}
	_ = c.ph.Enqueue(posthog.Capture{
		DistinctId: c.distinctID,
		Event:      "AUTH_LOGIN_STARTED",
		Properties: c.baseProperties("auth login").Set("method", method),
	})
}

// AuthLoginCompleted records a successful login for the given method.
func (c *Client) AuthLoginCompleted(method string) {
	if !c.enabled || c.ph == nil {
		return
	}
	_ = c.ph.Enqueue(posthog.Capture{
		DistinctId: c.distinctID,
		Event:      "AUTH_LOGIN_COMPLETED",
		Properties: c.baseProperties("auth login").Set("method", method),
	})
}

// AuthLoginFailed records a failed login attempt with a specific reason
// drawn from the login flow's real failure branches (see auth_login.go).
func (c *Client) AuthLoginFailed(method, reason string) {
	if !c.enabled || c.ph == nil {
		return
	}
	_ = c.ph.Enqueue(posthog.Capture{
		DistinctId: c.distinctID,
		Event:      "AUTH_LOGIN_FAILED",
		Properties: c.baseProperties("auth login").Set("method", method).Set("reason", reason),
	})
}

// sharedConfigPath returns ~/.hyperframes/config.json, the cross-tool
// install-identity file hyperframes CLI and media-use already read/write
// (skills/media-use/scripts/lib/telemetry.mjs: sharedConfigPath /
// anonymousId). Resolved independently of HEYGEN_CONFIG_DIR — it's a
// different, cross-tool directory by design, not heygen-cli's own config
// dir. Returns "" if the home directory can't be resolved; callers treat
// that the same as "file absent."
func sharedConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".hyperframes", "config.json")
}

// readSharedConfig reads and parses the shared config file, tolerating a
// missing or malformed file as "absent" (returns an empty map, never
// panics) so a corrupt file from another tool never breaks heygen-cli.
func readSharedConfig() map[string]any {
	path := sharedConfigPath()
	if path == "" {
		return map[string]any{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]any{}
	}
	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil || config == nil {
		return map[string]any{}
	}
	return config
}

// writeSharedConfig writes the full config object back to disk. Callers
// must read-merge-write (readSharedConfig, set only the key(s) this
// codebase owns, then writeSharedConfig the whole map) — this file is
// written by three independent codebases (hyperframes CLI, media-use, and
// heygen-cli), and a blind overwrite here would drop a field one of the
// others just wrote (e.g. telemetryNoticeShown).
func writeSharedConfig(config map[string]any) {
	path := sharedConfigPath()
	if path == "" {
		return
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	_ = os.WriteFile(path, append(data, '\n'), 0o600)
}

// legacyAnalyticsID reads heygen-cli's own pre-existing analytics id
// (~/.heygen/analytics_id, or HEYGEN_CONFIG_DIR's override) from before
// this codebase adopted the shared install identity. Read-only: nothing
// writes to this file anymore.
func legacyAnalyticsID() string {
	idPath := filepath.Join(paths.ConfigDir(), "analytics_id")
	data, err := os.ReadFile(idPath)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// distinctID resolves heygen-cli's PostHog distinct id from the shared
// hyperframes install identity (~/.hyperframes/config.json's anonymousId),
// the same cross-tool id hyperframes CLI and media-use already use.
//
//   - Shared config has anonymousId → use it verbatim.
//   - Shared config absent (or malformed) but a legacy heygen-cli-only id
//     exists → promote it into the shared config so an upgrading user
//     keeps their identity, then use it.
//   - Neither exists → mint a fresh id and write it to the shared config.
//
// Every write is read-merge-write via readSharedConfig/writeSharedConfig,
// so a field another tool wrote to the same file survives.
func distinctID() string {
	config := readSharedConfig()
	if id, ok := config["anonymousId"].(string); ok {
		if trimmed := strings.TrimSpace(id); trimmed != "" {
			return trimmed
		}
	}

	id := legacyAnalyticsID()
	if id == "" {
		id = uuid.NewString()
	}

	config["anonymousId"] = id
	writeSharedConfig(config)
	return id
}
