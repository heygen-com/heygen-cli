package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/heygen-com/heygen-cli/internal/command"
	clierrors "github.com/heygen-com/heygen-cli/internal/errors"
	"github.com/spf13/cobra"
)

var downloadClient = &http.Client{Timeout: 10 * time.Minute}

func newVideoDownloadCmd(ctx *cmdContext) *cobra.Command {
	var outputPath string

	cmd := &cobra.Command{
		Use:   "download <video-id>",
		Short: "Download a video file to disk",
		Args:  cobra.ExactArgs(1),
		Example: "heygen video download <video-id>\n" +
			"heygen video download <video-id> --output-path my-video.mp4",
		RunE: func(cmd *cobra.Command, args []string) error {
			videoID := args[0]

			spec := &command.Spec{
				Endpoint: "/v3/videos/{video_id}",
				Method:   http.MethodGet,
			}
			inv := &command.Invocation{
				PathParams:  map[string]string{"video_id": videoID},
				QueryParams: make(url.Values),
			}
			result, err := ctx.client.Execute(spec, inv)
			if err != nil {
				return err
			}

			videoURL, err := extractVideoURL(result, videoID)
			if err != nil {
				return err
			}

			dest := outputPath
			if dest == "" {
				// Sanitize: strip directory components from video ID to prevent
				// path traversal. Handles IDs with / or \ safely.
				dest = filepath.Base(videoID) + ".mp4"
			}

			if err := downloadFile(cmd.Context(), videoURL, dest); err != nil {
				return err
			}

			data, err := json.Marshal(map[string]string{
				"message": "Downloaded to " + dest,
				"path":    dest,
			})
			if err != nil {
				return clierrors.New(fmt.Sprintf("failed to encode response: %v", err))
			}

			return ctx.formatter.Data(data, "", nil)
		},
	}

	cmd.Flags().StringVar(&outputPath, "output-path", "", "Output file path (default: {video-id}.mp4)")
	return cmd
}

func extractVideoURL(raw json.RawMessage, videoID string) (string, error) {
	var resp struct {
		Data struct {
			VideoURL string `json:"video_url"`
			Status   string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", clierrors.New("failed to parse video response")
	}

	if resp.Data.VideoURL == "" {
		switch resp.Data.Status {
		case "failed", "error":
			return "", &clierrors.CLIError{
				Code:     "video_failed",
				Message:  fmt.Sprintf("video rendering failed (status: %s)", resp.Data.Status),
				Hint:     "Check details with: heygen video get " + videoID,
				ExitCode: clierrors.ExitGeneral,
			}
		default:
			msg := "video URL not available"
			if resp.Data.Status != "" {
				msg = fmt.Sprintf("video URL not available (status: %s)", resp.Data.Status)
			}
			return "", &clierrors.CLIError{
				Code:     "video_not_ready",
				Message:  msg,
				Hint:     "Use --wait when creating: heygen video create ... --wait",
				ExitCode: clierrors.ExitGeneral,
			}
		}
	}

	return resp.Data.VideoURL, nil
}

func downloadFile(ctx context.Context, videoURL, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, videoURL, nil)
	if err != nil {
		return clierrors.New(fmt.Sprintf("failed to build download request: %v", err))
	}

	resp, err := downloadClient.Do(req)
	if err != nil {
		return clierrors.New(fmt.Sprintf("failed to download video: %v", err))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return clierrors.New(fmt.Sprintf("download failed with HTTP %d", resp.StatusCode))
	}

	// Write to a temp file in the same directory, then rename on success.
	// This prevents destroying an existing file on partial download failure.
	dir := filepath.Dir(dest)
	tmp, err := os.CreateTemp(dir, ".heygen-download-*.tmp")
	if err != nil {
		return clierrors.New(fmt.Sprintf("failed to create temp file in %q: %v", dir, err))
	}
	tmpPath := tmp.Name()

	_, copyErr := io.Copy(tmp, resp.Body)
	closeErr := tmp.Close()
	if copyErr != nil {
		_ = os.Remove(tmpPath)
		return clierrors.New(fmt.Sprintf("download interrupted: %v", copyErr))
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return clierrors.New(fmt.Sprintf("failed to finalize download: %v", closeErr))
	}

	// Atomic rename. On Windows this may fail if dest is open elsewhere;
	// os.Rename across filesystems also fails, but temp file is in the
	// same directory so this is safe.
	if err := os.Rename(tmpPath, dest); err != nil {
		_ = os.Remove(tmpPath)
		return clierrors.New(fmt.Sprintf("failed to move download to %q: %v", dest, err))
	}

	return nil
}
