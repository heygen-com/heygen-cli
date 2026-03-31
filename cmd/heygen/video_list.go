package main

import (
	"fmt"
	"strconv"

	"github.com/heygen-com/heygen-cli/internal/client"
	clierrors "github.com/heygen-com/heygen-cli/internal/errors"
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
			// Validate --limit if explicitly provided
			if cmd.Flags().Changed("limit") {
				if limit < 1 || limit > 100 {
					return clierrors.NewUsage(fmt.Sprintf("--limit must be between 1 and 100, got %d", limit))
				}
			}

			spec := client.RequestSpec{
				Endpoint:   "/v3/videos",
				Method:     "GET",
				Paginated:  true,
				DataField:  "data",
				TokenField: "next_token",
			}

			if cmd.Flags().Changed("limit") {
				spec.QueryParams = append(spec.QueryParams, client.QueryParam{Key: "limit", Value: strconv.Itoa(limit)})
			}
			if token != "" {
				spec.QueryParams = append(spec.QueryParams, client.QueryParam{Key: "token", Value: token})
			}
			if folderID != "" {
				spec.QueryParams = append(spec.QueryParams, client.QueryParam{Key: "folder_id", Value: folderID})
			}

			result, err := ctx.client.Execute(spec)
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
