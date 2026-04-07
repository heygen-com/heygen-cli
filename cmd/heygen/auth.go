package main

import "github.com/spf13/cobra"

func newAuthCmd(ctx *cmdContext) *cobra.Command {
	cmd := newCommandGroup("auth", "Manage authentication")
	cmd.AddCommand(newAuthLoginCmd(ctx))
	cmd.AddCommand(newAuthStatusCmd(ctx))
	return cmd
}
