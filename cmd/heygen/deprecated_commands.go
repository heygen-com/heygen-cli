package main

import (
	"fmt"

	"github.com/heygen-com/heygen-cli/internal/command"
	"github.com/heygen-com/heygen-cli/internal/output"
)

// deprecatedCommandReplacements names the command to move to, keyed by the
// deprecated operation's "METHOD /path". The spec marks an operation
// deprecated but carries no successor, so this is CLI-side lookup data.
// An entry is optional: a deprecated command without one gets the generic
// notice.
var deprecatedCommandReplacements = map[string]string{
	"GET /v3/voices":               "model audio voices list",
	"POST /v3/voices":              "model audio voices create",
	"POST /v3/voices/clone":        "model audio voices create",
	"GET /v3/voices/{voice_id}":    "model audio voices get",
	"DELETE /v3/voices/{voice_id}": "model audio voices delete",
	"POST /v3/voices/speech":       "model audio tts create",
}

// deprecatedCommandNotice is the warning a deprecated command prints when run,
// and the first line of its own --help.
func deprecatedCommandNotice(spec *command.Spec) string {
	msg := fmt.Sprintf("%q is deprecated and will be removed in a future version", "heygen "+spec.Group+" "+spec.Name)
	if replacement := deprecatedCommandReplacements[spec.EndpointKey()]; replacement != "" {
		msg += fmt.Sprintf("; use %q instead", "heygen "+replacement)
	}
	return msg
}

// warnDeprecatedCommand goes through the formatter, like warnDeprecatedFlags,
// so the notice lands on stderr in the warning envelope and never on stdout.
func warnDeprecatedCommand(spec *command.Spec, f output.Formatter) {
	if spec.Deprecated {
		f.Warn(deprecatedCommandNotice(spec))
	}
}
