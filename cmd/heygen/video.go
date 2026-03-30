package main

import "github.com/spf13/cobra"

func newVideoCmd(ctx *cmdContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "video",
		Short: "Manage videos",
	}
	cmd.AddCommand(newVideoListCmd(ctx))
	return cmd
}
