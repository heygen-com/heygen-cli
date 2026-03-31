package main

import (
	"github.com/heygen-com/heygen-cli/gen"
	"github.com/spf13/cobra"
)

// registerGenCommands adds all generated command groups to the root command.
// Each group becomes a Cobra subcommand (e.g., "video", "avatar"), and each
// spec within the group becomes a leaf command (e.g., "video list", "avatar get").
func registerGenCommands(root *cobra.Command, ctx *cmdContext) {
	for groupName, specs := range gen.Groups {
		groupCmd := &cobra.Command{
			Use:   groupName,
			Short: "Manage " + groupName,
		}
		for _, spec := range specs {
			groupCmd.AddCommand(buildGenCommand(spec, ctx))
		}
		root.AddCommand(groupCmd)
	}
}
