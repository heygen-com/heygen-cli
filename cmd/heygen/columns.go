package main

import "github.com/heygen-com/heygen-cli/internal/command"

// DefaultColumns defines curated table columns for --human mode.
// Keys use the full generated command path: "group/spec.Name".
var DefaultColumns = map[string][]command.Column{}

func defaultColumnsForSpec(spec *command.Spec) []command.Column {
	return DefaultColumns[spec.Group+"/"+spec.Name]
}
