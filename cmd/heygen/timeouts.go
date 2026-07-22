package main

import (
	"net/http"
	"time"

	"github.com/heygen-com/heygen-cli/internal/client"
	"github.com/heygen-com/heygen-cli/internal/command"
)

// Per-operation HTTP request timeouts. A single global timeout is wrong here:
// server-side latency differs by an order of magnitude across operation classes
// (reads p99 < 2s; writes run to a p99 of ~30s because they do synchronous work
// and some enqueue async jobs; uploads are bounded by file size and client
// bandwidth, not server compute). Deliberately not user-configurable yet — add
// an override only when a real case needs one.
const (
	// timeoutRead covers GET reads and lists (server p99 < 2s). Tracks the
	// client's built-in default so there is a single source of truth for 30s.
	timeoutRead = client.DefaultTimeout
	// timeoutWrite covers every mutating call (POST/PUT/PATCH/DELETE): creates
	// that do synchronous work or enqueue async jobs, plus other writes. Sized
	// off the slowest observed case — POST /v3/videos runs to a Datadog p99 of
	// ~31s (30d window) — with roughly 4x headroom before giving up.
	timeoutWrite = 120 * time.Second
	// timeoutUpload covers multipart uploads, bounded by file size and client
	// bandwidth. Matches the dedicated video-download client budget.
	timeoutUpload = 10 * time.Minute
)

// timeoutForSpec picks the per-request HTTP timeout for a generated command by
// operation class, keyed on the spec's HTTP method and body encoding — signals
// that live on the immutable generated Spec. Method-driven, not coupled to the
// hand-written poll-config set (which covers only 4 of the async-create
// endpoints), so the classification can't silently drift as commands are added.
// A runtime enhancement, kept out of the Spec like poll configs and --human
// columns.
func timeoutForSpec(spec *command.Spec) time.Duration {
	if spec.BodyEncoding == "multipart" {
		return timeoutUpload
	}
	if spec.Method == http.MethodGet {
		return timeoutRead
	}
	// Every non-GET, non-upload call is a write (create / update / delete).
	// Creates in particular are slow-synchronous or async-submit server-side.
	return timeoutWrite
}
