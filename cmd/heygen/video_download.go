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

type assetInfo struct {
	field string
	ext   string
	label string
}

var assetTypes = map[string]assetInfo{
	"video":     {field: "video_url", ext: ".mp4", label: "video"},
	"captioned": {field: "captioned_video_url", ext: ".mp4", label: "captioned video"},
}

func newVideoDownloadCmd(ctx *cmdContext) *cobra.Command {
	var outputPath string
	var asset string
	var force bool

	cmd := &cobra.Command{
		Use:   "download <video-id>",
		Short: "Download a video file or related asset to disk",
		Args:  cobra.ExactArgs(1),
		Example: "heygen video download <video-id>\n" +
			"heygen video download <video-id> --asset captioned\n" +
			"heygen video download <video-id> --output-path my-video.mp4",
		RunE: func(cmd *cobra.Command, args []string) error {
			videoID := args[0]
			info, ok := assetTypes[asset]
			if !ok {
				return clierrors.NewUsage(
					fmt.Sprintf("invalid --asset value %q: must be one of: video, captioned", asset))
			}

			dest := outputPath
			if dest == "" {
				// Sanitize: strip directory components from video ID to prevent
				// path traversal. Handles IDs with / or \\ safely.
				dest = filepath.Base(videoID) + info.ext
			}

			if !force {
				if _, err := os.Stat(dest); err == nil {
					if !stdinIsTerminalFunc() {
						return &clierrors.CLIError{
							Code:     "file_exists",
							Message:  fmt.Sprintf("file %q already exists", dest),
							Hint:     "Use --force to overwrite, or --output-path to write to a different file",
							ExitCode: clierrors.ExitGeneral,
						}
					}
					if err := confirmAction(
						cmd.ErrOrStderr(),
						cmd.InOrStdin(),
						fmt.Sprintf("File %q already exists. Overwrite?", dest),
					); err != nil {
						return err
					}
				}
			}

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

			assetURL, err := extractAssetURL(result, videoID, info)
			if err != nil {
				return err
			}

			if err := downloadFile(cmd.Context(), assetURL, dest); err != nil {
				return err
			}

			data, err := json.Marshal(map[string]string{
				"asset":   asset,
				"message": fmt.Sprintf("Downloaded %s to %s", info.label, dest),
				"path":    dest,
			})
			if err != nil {
				return &clierrors.CLIError{
					Code:     "cli_response_encode_error",
					Message:  fmt.Sprintf("failed to encode response: %v", err),
					Hint:     "Please report this CLI bug with the command you ran.",
					ExitCode: clierrors.ExitGeneral,
				}
			}

			return ctx.formatter.Data(data, "", nil)
		},
	}

	cmd.Flags().StringVar(&asset, "asset", "video", "Asset to download: video, captioned")
	cmd.Flags().StringVar(&outputPath, "output-path", "", "Output file path (default: {video-id}.mp4)")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite existing files without prompting")
	return cmd
}

func extractAssetURL(raw json.RawMessage, videoID string, info assetInfo) (string, error) {
	var resp struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", &clierrors.CLIError{
			Code:     "cli_response_parse_error",
			Message:  "failed to parse the video response",
			Hint:     "The API response could not be parsed. Retry; if it persists, report it (possible CLI/API mismatch).",
			ExitCode: clierrors.ExitGeneral,
		}
	}

	var status string
	if rawStatus, ok := resp.Data["status"]; ok {
		_ = json.Unmarshal(rawStatus, &status)
	}

	var assetURL string
	if rawURL, ok := resp.Data[info.field]; ok {
		_ = json.Unmarshal(rawURL, &assetURL)
	}

	if assetURL == "" {
		switch status {
		case "failed", "error":
			return "", &clierrors.CLIError{
				Code:     "video_failed",
				Message:  fmt.Sprintf("video rendering failed (status: %s)", status),
				Hint:     "Check details with: heygen video get " + videoID,
				ExitCode: clierrors.ExitGeneral,
			}
		case "completed":
			return "", &clierrors.CLIError{
				Code:     "asset_not_available",
				Message:  fmt.Sprintf("%s not available for this video", info.label),
				Hint:     assetHint(info.field),
				ExitCode: clierrors.ExitGeneral,
			}
		default:
			msg := fmt.Sprintf("%s URL not available", info.label)
			if status != "" {
				msg = fmt.Sprintf("%s URL not available (status: %s)", info.label, status)
			}
			return "", &clierrors.CLIError{
				Code:     "video_not_ready",
				Message:  msg,
				Hint:     "Use --wait when creating: heygen video create ... --wait",
				ExitCode: clierrors.ExitGeneral,
			}
		}
	}

	return assetURL, nil
}

func assetHint(field string) string {
	switch field {
	case "captioned_video_url":
		return "Video may not have been created with captions enabled."
	default:
		return ""
	}
}

func downloadFile(ctx context.Context, videoURL, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, videoURL, nil)
	if err != nil {
		return &clierrors.CLIError{
			Code:     "cli_response_parse_error",
			Message:  fmt.Sprintf("the download URL returned by the API is unusable: %v", err),
			Hint:     "Re-fetch a fresh URL: heygen video get <video_id>",
			ExitCode: clierrors.ExitGeneral,
		}
	}

	resp, err := downloadClient.Do(req)
	if err != nil {
		// network_error is intentionally NOT cli_-prefixed. It is a shared code
		// also emitted by the API-executor transport path
		// (internal/client/executor.go); kept bare so both sites agree. Do not
		// prefix one site without the other.
		return &clierrors.CLIError{
			Code:     "network_error",
			Message:  fmt.Sprintf("failed to download video: %v", err),
			Hint:     "This is usually a temporary network issue. Check your connection and retry.",
			ExitCode: clierrors.ExitGeneral,
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// A presigned asset URL that 403s/404s is expired or revoked (client can
		// re-fetch); any other status is the asset host (our storage/CDN) failing.
		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound {
			return &clierrors.CLIError{
				Code:       "cli_download_url_expired",
				Message:    fmt.Sprintf("download link expired or unavailable (HTTP %d)", resp.StatusCode),
				Hint:       "This download link has expired. Re-fetch a fresh URL: heygen video get <video_id>",
				HTTPStatus: resp.StatusCode,
				ExitCode:   clierrors.ExitGeneral,
			}
		}
		return &clierrors.CLIError{
			Code:       "cli_download_failed",
			Message:    fmt.Sprintf("download failed with HTTP %d", resp.StatusCode),
			Hint:       "The asset host returned an error fetching the file. This is usually transient. Retry shortly.",
			HTTPStatus: resp.StatusCode,
			ExitCode:   clierrors.ExitGeneral,
		}
	}

	// Write to a temp file in the same directory, then rename on success.
	// This prevents destroying an existing file on partial download failure.
	dir := filepath.Dir(dest)
	tmp, err := os.CreateTemp(dir, ".heygen-download-*.tmp")
	if err != nil {
		return &clierrors.CLIError{
			Code:     "cli_file_io_error",
			Message:  fmt.Sprintf("failed to create temp file in %q: %v", dir, err),
			Hint:     "Could not write locally. Check the destination path and free disk space.",
			ExitCode: clierrors.ExitGeneral,
		}
	}
	tmpPath := tmp.Name()

	_, copyErr := io.Copy(tmp, resp.Body)
	closeErr := tmp.Close()
	if copyErr != nil {
		_ = os.Remove(tmpPath)
		return &clierrors.CLIError{
			Code:     "cli_download_interrupted",
			Message:  fmt.Sprintf("download interrupted: %v", copyErr),
			Hint:     "The transfer was cut off. Retry the download. The partial file was cleaned up.",
			ExitCode: clierrors.ExitGeneral,
		}
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return &clierrors.CLIError{
			Code:     "cli_file_io_error",
			Message:  fmt.Sprintf("failed to finalize download: %v", closeErr),
			Hint:     "Could not finalize the file locally. Check free disk space.",
			ExitCode: clierrors.ExitGeneral,
		}
	}

	// Atomic rename. On Windows this may fail if dest is open elsewhere;
	// os.Rename across filesystems also fails, but temp file is in the
	// same directory so this is safe.
	if err := os.Rename(tmpPath, dest); err != nil {
		_ = os.Remove(tmpPath)
		return &clierrors.CLIError{
			Code:     "cli_file_io_error",
			Message:  fmt.Sprintf("failed to move download to %q: %v", dest, err),
			Hint:     "Could not write to the destination path. Check permissions and free disk space.",
			ExitCode: clierrors.ExitGeneral,
		}
	}

	return nil
}
