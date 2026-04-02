package main

import "github.com/heygen-com/heygen-cli/internal/command"

// DefaultColumns defines curated table columns for --human mode.
// Keys use the full generated command path: "group/spec.Name".
var DefaultColumns = map[string][]command.Column{
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
		{Header: "Preview", Field: "preview_image_url"},
	},
	"voice/list": {
		{Header: "ID", Field: "voice_id"},
		{Header: "Name", Field: "name"},
		{Header: "Language", Field: "language"},
		{Header: "Gender", Field: "gender"},
	},
	"video-translate/list": {
		{Header: "ID", Field: "id"},
		{Header: "Title", Field: "title"},
		{Header: "Status", Field: "status"},
		{Header: "Created", Field: "created_at"},
	},
	"webhook/endpoints list": {
		{Header: "ID", Field: "endpoint_id"},
		{Header: "URL", Field: "url"},
		{Header: "Status", Field: "status"},
		{Header: "Created", Field: "created_at"},
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
	"overdub/list": {
		{Header: "ID", Field: "id"},
		{Header: "Title", Field: "title"},
		{Header: "Status", Field: "status"},
		{Header: "Created", Field: "created_at"},
	},
}

func defaultColumnsForSpec(spec *command.Spec) []command.Column {
	return DefaultColumns[spec.Group+"/"+spec.Name]
}
