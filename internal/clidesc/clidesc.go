// Package clidesc holds the CLI-surface description overlay: hand-written
// reframes of OpenAPI descriptions that read badly on the CLI surface, plus
// the resolvers the command builder uses to apply them.
//
// The CLI generates its command surface from HeyGen's OpenAPI spec. Some spec
// descriptions are written in raw HTTP terms ("poll via GET /v3/videos/{id}",
// "Pass to POST /v3/video-agents") that mislead a CLI user, who drives the API
// with commands and flags. This overlay reframes those few cases in CLI terms.
//
// This is a runtime overlay, NOT a codegen/spec change: command.Spec is
// generated and immutable (see AGENTS.md), so overrides are resolved at
// command-build time and never mutate the Spec. It mirrors the curated-lookup
// pattern used for --human columns (DefaultColumns in cmd/heygen/columns.go).
// It lives in internal/ (not gen/) so both the command builder and the
// advisory resync checker (make check-descriptions) share one source of truth.
package clidesc

import (
	"encoding/json"

	"github.com/heygen-com/heygen-cli/internal/command"
)

// Override is a CLI-surface description overlay for one generated command,
// keyed by the command's (Endpoint, Method) — the same pair that uniquely
// identifies a command.Spec.
//
// Every field is optional. An empty field falls through to the spec text
// unchanged. Most commands have NO override.
type Override struct {
	// Summary overrides cobra.Command.Short (the one-line help). Empty → spec.Summary.
	Summary string
	// Description overrides cobra.Command.Long (the long help). Empty → spec.Description.
	Description string
	// Flags overrides per-flag usage text, keyed by the flag's kebab-case name
	// (e.g. "brand-glossary-id"). A flag absent from this map keeps its
	// generated FlagSpec.Help.
	Flags map[string]string
	// Fields overrides schema-property descriptions surfaced by
	// --request-schema / --response-schema, keyed by the JSON property name
	// (e.g. "video_url", "style_id"). Applied recursively to every property of
	// that name in the schema JSON. A property absent from this map keeps its
	// generated description.
	Fields map[string]string
}

// Key identifies a command by its HTTP identity. (Endpoint, Method) uniquely
// identifies a command.Spec, so it is a stable key that survives command
// renames in the generated surface.
type Key struct {
	Endpoint string
	Method   string
}

// Overrides is the sparse overlay table. Keyed by endpoint+method. Only
// entries that genuinely diverge from CLI framing appear here; everything else
// falls through to the generated spec text.
//
// Precedence: this overlay wins over the generated spec text.
var Overrides = map[Key]Override{
	// brand glossaries list — reframe the POST /v2/video_translate cross-ref
	// (doubly wrong: CLI uses /v3/video-translations) and "this endpoint" →
	// "this command".
	{"/v3/brand-glossaries", "GET"}: {
		Description: "List brand glossaries (custom term mappings, a.k.a. brand voices) in the authenticated user's workspace. A brand glossary enforces custom term translations during video translation — for example, translating \"Reformer\" as Pilates equipment instead of as a political activist. Brand glossaries are created and edited in the HeyGen web app under Brand Kit; this command is read-only for discovery.\n\nPass the returned `brand_glossary_id` (or the legacy alias `brand_voice_id`) as `--brand-glossary-id` to `heygen video-translate create`.",
		Fields: map[string]string{
			// BrandGlossaryItem.brand_glossary_id
			"brand_glossary_id": "Unique brand glossary ID. Pass as `--brand-glossary-id` (or the legacy `--brand-voice-id`) to `heygen video-translate create` to apply the glossary during translation.",
		},
	},

	// brand kits list — POST /v3/video-agents cross-ref → CLI command.
	{"/v3/brand-kits", "GET"}: {
		Description: "Returns brand kits available in the authenticated user's workspace. Each brand kit contains colors, fonts, and logos that can be applied to Video Agent sessions. Pass the returned `brand_kit_id` as `--brand-kit-id` to `heygen video-agent create` to generate on-brand videos.",
		Fields: map[string]string{
			// BrandKitItem.brand_kit_id
			"brand_kit_id": "Unique brand kit identifier. Pass as `--brand-kit-id` to `heygen video-agent create`.",
		},
	},

	// voice speech create — GET /v3/voices?engine=starfish → CLI command.
	{"/v3/voices/speech", "POST"}: {
		Description: "Synthesize speech audio from text using a specified voice. The voice must support the starfish engine — run `heygen voice list --engine starfish` to find compatible voices. Supports plain text and SSML. Speed range: 0.5–2.0x. Returns a URL to the generated audio file along with duration and optional word-level timestamps.",
	},

	// voice clone create — poll via GET /v3/voices/{id} → heygen voice get
	// (no --wait on this command, so the reframe points only at voice get).
	{"/v3/voices/clone", "POST"}: {
		Description: "Creates a voice clone from an audio file. Returns a voice_clone_id; poll it with `heygen voice get <voice-clone-id>` until the status is 'complete'. The resulting voice can then be used with `heygen voice speech create` and `heygen video create`.",
	},

	// video get — surface the heygen video download command on the URL fields.
	{"/v3/videos/{video_id}", "GET"}: {
		Fields: map[string]string{
			"video_url":           "URL to download the video file. Tip: `heygen video download <video-id>` saves it to disk for you.",
			"captioned_video_url": "URL to download the video file with captions burned in. Tip: `heygen video download <video-id> --asset captioned` saves it to disk.",
		},
	},

	// video-translate create — flag help reframes.
	//
	// Flag help intentionally avoids backticks: Cobra/pflag treats the first
	// backtick-delimited token in a flag's usage string as the value-type
	// placeholder shown after the flag name (e.g. `--style-id <placeholder>`),
	// which would mangle the column. Backticks are fine in Summary/Description
	// (Long help) and in schema field descriptions, just not in flag usage.
	{"/v3/video-translations", "POST"}: {
		Flags: map[string]string{
			"output-languages":  "Target language names (e.g. 'Chinese (Cantonese, Traditional)', 'Spanish (Spain)', 'English'). Run 'heygen video-translate languages list' for valid values. Use one for single translation, multiple for batch.",
			"brand-glossary-id": "Brand glossary ID for custom term translations (e.g. translate 'Reformer' as the Pilates equipment, not 'political activist'). Alias for the legacy --brand-voice-id flag. Discover IDs via 'heygen brand glossaries list'.",
			"brand-voice-id":    "Brand glossary ID for custom term translations. Legacy alias for --brand-glossary-id — both are accepted and resolve to the same workspace record. Discover IDs via 'heygen brand glossaries list'.",
		},
	},

	// video-translate proofreads create — brand-glossary-id flag help.
	{"/v3/video-translations/proofreads", "POST"}: {
		Flags: map[string]string{
			"brand-glossary-id": "Brand glossary ID for custom term translations (e.g. translate 'Reformer' as 'Pilates equipment', not 'political activist'). Alias for the legacy --brand-voice-id flag. Discover IDs via 'heygen brand glossaries list'.",
		},
	},

	// video-translate proofreads generate — video_translation_id poll reframe.
	{"/v3/video-translations/proofreads/{proofread_id}/generate", "POST"}: {
		Fields: map[string]string{
			"video_translation_id": "Video translation ID — poll status with `heygen video-translate get <video-translation-id>`, or pass `--wait` on create.",
		},
	},

	// lipsync create — lipsync_id poll reframe.
	{"/v3/lipsyncs", "POST"}: {
		Fields: map[string]string{
			"lipsync_id": "Lipsync ID — poll status with `heygen lipsync get <lipsync-id>`, or pass `--wait` on create.",
		},
	},

	// avatar looks get/list — AvatarLookItem.id / .supported_api_engines.
	// Both commands return the same AvatarLookItem model, so both are keyed.
	{"/v3/avatars/looks/{look_id}", "GET"}: {
		Fields: avatarLookItemFields,
	},
	{"/v3/avatars/looks", "GET"}: {
		Fields: avatarLookItemFields,
	},

	// video-agent create — style-id flag + CreateVideoAgentResponse.video_id.
	{"/v3/video-agents", "POST"}: {
		Flags: map[string]string{
			"style-id": "Style ID from 'heygen video-agent styles list'. Applies a curated visual template to the generated video.",
		},
		Fields: map[string]string{
			"video_id": "Video ID — poll status with `heygen video get <video-id>`, or pass `--wait` on `heygen video-agent create`. Nullable in future multi-turn flows.",
		},
	},

	// video-agent styles list — StyleItem.style_id.
	{"/v3/video-agents/styles", "GET"}: {
		Fields: map[string]string{
			"style_id": "Unique style identifier. Pass as `--style-id` to `heygen video-agent create`.",
		},
	},

	// asset direct-uploads create — reframe the /complete and /v3/assets
	// cross-refs to CLI commands (the raw S3 PUT stays, it is genuinely
	// unwrapped by the CLI).
	{"/v3/assets/direct-uploads", "POST"}: {
		Description: "Begin a direct-to-S3 upload. Returns an `asset_id` and a presigned `upload_url`; PUT the file bytes to `upload_url` yourself (e.g. with curl), then run `heygen asset complete create <asset-id>` to finalize. Unlike `heygen asset create` (which proxies the bytes), this never sends the file through HeyGen.",
		Fields: map[string]string{
			// CreateAssetUploadResponse.asset_id
			"asset_id": "Reusable asset identifier. Becomes usable after `heygen asset complete create <asset-id>`.",
		},
	},

	// asset create — UploadAssetV3Response.asset_id.
	{"/v3/assets", "POST"}: {
		Fields: map[string]string{
			"asset_id": "Unique asset identifier for use in other commands, e.g. as an `asset_id` reference in `heygen video-agent create` or `heygen video create`.",
		},
	},
}

// avatarLookItemFields is the AvatarLookItem field overlay shared by
// `avatar looks get` and `avatar looks list` (both return AvatarLookItem).
var avatarLookItemFields = map[string]string{
	"id":                    "Unique look identifier. Pass this as `avatar_id` (in the `--data`/`-d` JSON) to `heygen video create`.",
	"supported_api_engines": "Engine values this look supports for `heygen video create`.",
}

// ForSpec returns the overlay for a spec, keyed by its HTTP identity, and
// whether one exists. Resolved at build time; the Spec is never mutated.
func ForSpec(spec *command.Spec) (Override, bool) {
	o, ok := Overrides[Key{spec.Endpoint, spec.Method}]
	return o, ok
}

// Summary returns the CLI Short text: the overlay summary if set, else the
// generated spec summary.
func Summary(spec *command.Spec) string {
	if o, ok := ForSpec(spec); ok && o.Summary != "" {
		return o.Summary
	}
	return spec.Summary
}

// Description returns the CLI Long text: the overlay description if set, else
// the generated spec description.
func Description(spec *command.Spec) string {
	if o, ok := ForSpec(spec); ok && o.Description != "" {
		return o.Description
	}
	return spec.Description
}

// FlagHelp returns the usage text for a flag: the overlay value if present,
// else the generated FlagSpec.Help.
func FlagHelp(spec *command.Spec, flag command.FlagSpec) string {
	if o, ok := ForSpec(spec); ok {
		if help, found := o.Flags[flag.Name]; found {
			return help
		}
	}
	return flag.Help
}

// Schema returns the schema JSON for --request-schema / --response-schema with
// any field-description overrides applied. The input schema string is never
// mutated in place; when overrides apply, a rewritten copy is returned. When no
// Fields override exists (the common case) or the schema cannot be parsed, the
// original string is returned unchanged.
func Schema(spec *command.Spec, schema string) string {
	o, ok := ForSpec(spec)
	if !ok || len(o.Fields) == 0 || schema == "" {
		return schema
	}

	var doc any
	if err := json.Unmarshal([]byte(schema), &doc); err != nil {
		// Faithful fallback: emit the original schema untouched.
		return schema
	}
	applyFieldOverrides(doc, o.Fields)

	// Re-marshal with indentation to match the human-readable schema output.
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return schema
	}
	return string(out)
}

// applyFieldOverrides walks a decoded JSON schema and, for every "properties"
// object, replaces the "description" of any property whose name appears in
// fields. Recurses into nested objects so fields under a wrapping "data"
// property are reached.
func applyFieldOverrides(node any, fields map[string]string) {
	switch v := node.(type) {
	case map[string]any:
		if props, ok := v["properties"].(map[string]any); ok {
			for name, prop := range props {
				if propMap, ok := prop.(map[string]any); ok {
					if override, found := fields[name]; found {
						propMap["description"] = override
					}
				}
			}
		}
		for _, child := range v {
			applyFieldOverrides(child, fields)
		}
	case []any:
		for _, child := range v {
			applyFieldOverrides(child, fields)
		}
	}
}
