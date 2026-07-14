package main

import "github.com/heygen-com/heygen-cli/internal/command"

// DefaultColumns defines curated table columns for --human mode.
// Keys use the full generated command path: "group/spec.Name".
var DefaultColumns = map[string][]command.Column{
	"video/statuses list": {
		{Header: "Video ID", Field: "video_id"},
		{Header: "Status", Field: "status"},
		{Header: "Batch ID", Field: "batch_id"},
	},
	"template/list": {
		{Header: "ID", Field: "id"},
		{Header: "Name", Field: "name"},
		{Header: "Aspect Ratio", Field: "aspect_ratio"},
		{Header: "Created", Field: "created_at"},
	},
	"ai-clipping/list": {
		{Header: "ID", Field: "id"},
		{Header: "Status", Field: "status"},
		{Header: "Progress", Field: "progress"},
		{Header: "Title", Field: "title"},
		{Header: "Created", Field: "created_at"},
	},
	"video/list": {
		{Header: "ID", Field: "id"},
		{Header: "Title", Field: "title"},
		{Header: "Status", Field: "status"},
		{Header: "Created", Field: "created_at"},
	},
	"avatar/list": {
		{Header: "ID", Field: "id"},
		{Header: "Name", Field: "name"},
		{Header: "Gender", Field: "gender"},
		{Header: "Looks", Field: "looks_count"},
	},
	"avatar/looks list": {
		{Header: "ID", Field: "id"},
		{Header: "Name", Field: "name"},
		{Header: "Type", Field: "avatar_type"},
		{Header: "Gender", Field: "gender"},
	},
	"voice/list": {
		{Header: "ID", Field: "voice_id"},
		{Header: "Name", Field: "name"},
		{Header: "Language", Field: "language"},
		{Header: "Gender", Field: "gender"},
	},
	"audio/sounds list": {
		{Header: "ID", Field: "id"},
		{Header: "Name", Field: "name"},
		{Header: "Duration", Field: "duration"},
		{Header: "Score", Field: "score"},
	},
	"video-translate/list": {
		{Header: "ID", Field: "id"},
		{Header: "Language", Field: "output_language"},
		{Header: "Status", Field: "status"},
		{Header: "Title", Field: "title"},
	},
	"webhook/endpoints list": {
		{Header: "ID", Field: "endpoint_id"},
		{Header: "URL", Field: "url"},
		{Header: "Events", Field: "events"},
		{Header: "Status", Field: "status"},
	},
	"webhook/event-types list": {
		{Header: "Event Type", Field: "event_type"},
		{Header: "Description", Field: "description"},
	},
	"webhook/events list": {
		{Header: "Event ID", Field: "event_id"},
		{Header: "Event Type", Field: "event_type"},
		{Header: "Created", Field: "created_at"},
	},
	"lipsync/list": {
		{Header: "ID", Field: "id"},
		{Header: "Title", Field: "title"},
		{Header: "Status", Field: "status"},
		{Header: "Created", Field: "created_at"},
	},
	"brand/kits list": {
		{Header: "ID", Field: "brand_kit_id"},
		{Header: "Name", Field: "name"},
		{Header: "Logo", Field: "logo_url"},
	},
	"brand/glossaries list": {
		{Header: "ID", Field: "brand_glossary_id"},
		{Header: "Name", Field: "name"},
		{Header: "Created", Field: "created_at"},
		{Header: "Updated", Field: "updated_at"},
	},
}

func defaultColumnsForSpec(spec *command.Spec) []command.Column {
	return DefaultColumns[spec.Group+"/"+spec.Name]
}
