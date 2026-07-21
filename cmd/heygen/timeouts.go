package main

import (
	"time"

	"github.com/heygen-com/heygen-cli/internal/client"
	"github.com/heygen-com/heygen-cli/internal/command"
)

// Per-operation HTTP request timeouts. A single global timeout is wrong here:
// server-side latency differs by an order of magnitude across operation classes
// (reads p99 < 2s; create endpoints p99 ~30s because they do synchronous work;
// uploads are bounded by file size and client bandwidth, not server compute).
// These are deliberately not user-configurable yet — add an override only when
// a real case needs one.
const (
	// timeoutRead covers reads/lists and quick POSTs. Matches the client default.
	timeoutRead = client.DefaultTimeout
	// timeoutCreate covers create/enqueue endpoints, which run to a p99 of ~30s
	// server-side; 120s clears that with headroom before giving up.
	timeoutCreate = 120 * time.Second
	// timeoutUpload covers multipart uploads, gated by file size and bandwidth.
	// Matches the dedicated video-download client budget.
	timeoutUpload = 10 * time.Minute
)

// timeoutForSpec picks the per-request HTTP timeout for a generated command by
// operation class. A runtime enhancement kept out of the immutable generated
// Spec, like poll configs and --human columns.
func timeoutForSpec(spec *command.Spec) time.Duration {
	if spec.BodyEncoding == "multipart" {
		return timeoutUpload
	}
	// Create/enqueue endpoints are exactly the ones with a poll config.
	if pollConfigs[spec.Group+"/"+spec.Name] != nil {
		return timeoutCreate
	}
	return timeoutRead
}
