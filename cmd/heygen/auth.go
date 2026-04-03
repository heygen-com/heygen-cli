package main

import "github.com/spf13/cobra"

func newAuthCmd(ctx *cmdContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage authentication",
	}
	cmd.AddCommand(newAuthLoginCmd(ctx))
	cmd.AddCommand(newAuthStatusCmd(ctx))
	return cmd
}
