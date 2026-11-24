package main

import (
	"maps"

	"github.com/heygen-com/heygen-cli/internal/command"
)

// defaultAvatarEngine is the engine `video create` asks for when the caller
// names none. The API's own default is unchanged; this is a CLI choice.
const defaultAvatarEngine = "heygen-avatar-6-turbo"

// applyDefaultEngine sets the default engine on a `video create` body that
// leaves the choice open, and keeps the body as written in FallbackBody so the
// executor can send that instead if the API refuses the default (a plan
// without 1080p, an avatar the engine cannot render).
//
// A body that names an engine, or asks for something the default engine does
// not take (a resolution other than 1080p, expressiveness), is sent as written.
// Batches are left alone: their items are created after the call returns, so
// there is no refusal to fall back from.
func applyDefaultEngine(spec *command.Spec, inv *command.Invocation) {
	if spec.Group != "video" || spec.Name != "create" || !takesDefaultEngine(inv.Body) {
		return
	}
	inv.FallbackBody = inv.Body
	inv.FallbackNotice = "warning: " + defaultAvatarEngine + " is not available for this request; sent without a default engine"
	inv.Body = maps.Clone(inv.Body)
	inv.Body["engine"] = map[string]any{"type": defaultAvatarEngine}
}

func takesDefaultEngine(body map[string]any) bool {
	if body["type"] != "avatar" && body["type"] != "image" {
		return false
	}
	if _, set := body["engine"]; set {
		return false
	}
	if _, set := body["expressiveness"]; set {
		return false
	}
	resolution, set := body["resolution"]
	return !set || resolution == "1080p"
}

// defaultEngineHelp is the `video create` help paragraph for the CLI default.
// The generated description above it describes the API's own default.
func defaultEngineHelp(spec *command.Spec) string {
	if spec.Group != "video" || spec.Name != "create" {
		return ""
	}
	return "\n\nCLI default (replaces the API default above): when the request names no `engine`, the CLI sends `" + defaultAvatarEngine +
		"`. The default is not applied when the request sets `engine`, `expressiveness`, or a `resolution` other than 1080p." +
		" If the API refuses the default, the CLI resends the request as written and prints a warning."
}
