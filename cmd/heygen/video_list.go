package main

import (
	"net/url"
	"strconv"

	"github.com/heygen-com/heygen-cli/internal/command"
	"github.com/spf13/cobra"
)

func newVideoListCmd(ctx *cmdContext) *cobra.Command {
	var limit int
	var token string
	var folderID string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List videos",
		Long:  "List videos in your HeyGen account with optional filtering.",
		RunE: func(cmd *cobra.Command, args []string) error {
			spec := &command.Spec{
				Endpoint:   "/v3/videos",
				Method:     "GET",
				TokenField: "next_token",
				DataField:  "data",
			}

			inv := &command.Invocation{
				PathParams:  make(map[string]string),
				QueryParams: make(url.Values),
			}

			if cmd.Flags().Changed("limit") {
				inv.QueryParams.Set("limit", strconv.Itoa(limit))
			}
			if token != "" {
				inv.QueryParams.Set("token", token)
			}
			if folderID != "" {
				inv.QueryParams.Set("folder_id", folderID)
			}

			result, err := ctx.client.Execute(spec, inv)
			if err != nil {
				return err
			}

			return ctx.formatter.Data(result)
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 0, "Maximum number of videos to return (1-100)")
	cmd.Flags().StringVar(&token, "token", "", "Pagination cursor from previous response")
	cmd.Flags().StringVar(&folderID, "folder-id", "", "Filter by folder ID")

	return cmd
}
