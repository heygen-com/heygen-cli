package analytics

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/heygen-com/heygen-cli/internal/paths"
	"github.com/posthog/posthog-go"
)

const posthogAPIKey = "phc_Bvsz7U8BvVxZtguuTGiBcbRUCkX46MwqdZ9t8LQ7cBGr"
const posthogEndpoint = "https://us.i.posthog.com"

type captureClient interface {
	Enqueue(posthog.Message) error
	Close() error
}

// Client wraps PostHog event capture behind a small no-op-friendly surface.
type Client struct {
	ph         captureClient
	enabled    bool
	distinctID string
	version    string
	started    bool
}

// New creates an analytics client. Disabled clients are inert no-ops.
func New(version string, enabled bool) *Client {
	if !enabled || posthogAPIKey == "" || strings.Contains(posthogAPIKey, "<project-token") {
		return &Client{}
	}

	ph, err := posthog.NewWithConfig(posthogAPIKey, posthog.Config{
		BatchSize: 1,
		Endpoint:  posthogEndpoint,
	})
	if err != nil {
		return &Client{}
	}

	return newWithCapture(version, ph)
}

func newWithCapture(version string, ph captureClient) *Client {
	return &Client{
		ph:         ph,
		enabled:    true,
		distinctID: distinctID(),
		version:    version,
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
		Properties: posthog.NewProperties().
			Set("command", command).
			Set("cli_version", c.version).
			Set("os", runtime.GOOS).
			Set("arch", runtime.GOARCH),
	})
}

func (c *Client) CommandRunComplete(command string, exitCode int, duration time.Duration) {
	if !c.enabled || c.ph == nil {
		return
	}
	_ = c.ph.Enqueue(posthog.Capture{
		DistinctId: c.distinctID,
		Event:      "COMMAND_RUN_COMPLETE",
		Properties: posthog.NewProperties().
			Set("command", command).
			Set("cli_version", c.version).
			Set("os", runtime.GOOS).
			Set("arch", runtime.GOARCH).
			Set("exit_code", exitCode).
			Set("duration_ms", duration.Milliseconds()).
			Set("success", exitCode == 0),
	})
}

func (c *Client) Close() {
	if c.enabled && c.ph != nil {
		_ = c.ph.Close()
	}
}

func (c *Client) Started() bool {
	return c.started
}

func distinctID() string {
	idPath := filepath.Join(paths.ConfigDir(), "analytics_id")
	if data, err := os.ReadFile(idPath); err == nil {
		if id := strings.TrimSpace(string(data)); id != "" {
			return id
		}
	}

	id := uuid.NewString()
	_ = os.MkdirAll(filepath.Dir(idPath), 0o700)
	_ = os.WriteFile(idPath, []byte(id), 0o600)
	return id
}
