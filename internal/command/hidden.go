package command

import "strings"

// hiddenEndpoints lists commands that remain fully functional but are omitted from `--help`
// listings (Cobra Hidden) and from generated docs — for endpoints that exist in the OpenAPI spec
// but aren't announced yet (still under development). The spec is the source of truth for what
// exists; this hand-maintained list is the CLI-side decision of what's discoverable. A hidden
// command still runs if invoked directly, and its own `--help` works. Keyed by "METHOD /path" so
// it survives command renames (e.g. an x-cli-action verb change).
//
// This list lives here (not in the builder) so every consumer of a Spec shares one source of
// truth: the runtime builder hides it from `--help`, and the docs generator omits it from public
// docs. To surface a command, remove its entry here. This list is transitional, for endpoints
// under development pre-launch; for a permanent hidden posture, plumb an `x-cli-hidden` extension
// from the OpenAPI spec so the spec stays the source of truth.
var hiddenEndpoints = map[string]bool{
	"GET /v3/assets/search": true, // asset search — pre-launch, not yet announced
}

// IsHidden reports whether the command for this spec should be omitted from help listings and
// generated docs.
func (s Spec) IsHidden() bool {
	return hiddenEndpoints[strings.ToUpper(s.Method)+" "+s.Endpoint]
}
