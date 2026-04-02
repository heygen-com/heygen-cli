package main

import "github.com/spf13/cobra"

func newAuthCmd(ctx *cmdContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "auth",
		Short:       "Manage authentication",
		Annotations: map[string]string{"skipAuth": "true"},
	}
	cmd.AddCommand(newAuthLoginCmd(ctx))
	return cmd
}
