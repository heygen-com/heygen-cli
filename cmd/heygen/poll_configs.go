package main

import "github.com/heygen-com/heygen-cli/internal/command"

// pollConfigs maps "group/spec.Name" to PollConfig for async commands.
// Keys use the full spec name to avoid collisions within a group.
var pollConfigs = map[string]*command.PollConfig{
	"video/create": {
		StatusEndpoint: "/v3/videos/{video_id}",
		StatusField:    "data.status",
		TerminalOK:     []string{"completed"},
		TerminalFail:   []string{"failed"},
		IDField:        "data.video_id",
		HintCommand:    "video get",
	},
	// The create response returns video_translation_ids (plural, always a list
	// even for single-language requests). We extract the first element with ".0".
	// Batch mode (multiple languages) is not supported by --wait.
	"video-translate/create": {
		StatusEndpoint: "/v3/video-translations/{video_translation_id}",
		StatusField:    "data.status",
		TerminalOK:     []string{"completed"},
		TerminalFail:   []string{"failed"},
		IDField:        "data.video_translation_ids.0",
		HintCommand:    "video-translate get",
	},
	// video-agent returns session_id + video_id. We poll the video status.
	// video_id can be null in future multi-turn flows — extractJSONPath
	// will return an error, which is correct (can't poll without an ID).
	"video-agent/create": {
		StatusEndpoint: "/v3/videos/{video_id}",
		StatusField:    "data.status",
		TerminalOK:     []string{"completed"},
		TerminalFail:   []string{"failed"},
		IDField:        "data.video_id",
		HintCommand:    "video get",
	},
}
