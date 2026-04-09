package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/heygen-com/heygen-cli/internal/command"
	clierrors "github.com/heygen-com/heygen-cli/internal/errors"
)

const (
	// APIDataField is the response envelope field containing the payload.
	// Exported so the builder can pass it to the formatter for --human rendering.
	APIDataField = "data"
)

// PollOptions controls ExecuteAndPoll behavior.
type PollOptions struct {
	Timeout   time.Duration
	BaseDelay time.Duration
	MaxDelay  time.Duration
	OnStatus  func(status string, elapsed time.Duration)
}

// ErrPollFailed is returned when polling reaches a terminal failure state.
// It carries the full status response so callers can output it (the response
// often contains error details the user needs to diagnose the failure).
type ErrPollFailed struct {
	Data   json.RawMessage
	Status string
}

func (e *ErrPollFailed) Error() string {
	return fmt.Sprintf("operation reached terminal failure state: %s", e.Status)
}

// ErrPollTimeout is returned when polling times out after the resource has
// been created but before a terminal status is reached.
type ErrPollTimeout struct {
	Data       json.RawMessage
	ResourceID string
}

func (e *ErrPollTimeout) Error() string {
	return fmt.Sprintf("polling timed out (resource %s still in progress)", e.ResourceID)
}

// ExecuteAndPoll executes a create request, then polls a status endpoint until
// the resource reaches a terminal state or the context is canceled.
func (c *Client) ExecuteAndPoll(
	ctx context.Context,
	spec *command.Spec,
	inv *command.Invocation,
	opts PollOptions,
) (json.RawMessage, error) {
	if spec.PollConfig == nil {
		return nil, clierrors.New("spec is not configured for polling")
	}

	opts = defaultPollOptions(opts)
	pollCtx, cancel := ensurePollContext(ctx, opts.Timeout)
	defer cancel()

	createResp, err := c.executeWithContext(pollCtx, spec, inv)
	if err != nil {
		return nil, translateCreateContextError(pollCtx, err)
	}

	resourceID, err := extractJSONPath(createResp, spec.PollConfig.IDField)
	if err != nil {
		return nil, clierrors.New(fmt.Sprintf(
			"failed to extract resource ID from %q: %v. This command may require manual polling for batch responses",
			spec.PollConfig.IDField, err,
		))
	}

	// If IDField uses an array index (e.g., "data.ids.0"), reject batch
	// responses with multiple IDs. --wait only supports single-resource polling.
	if arrayLen, ok := extractArrayLen(createResp, spec.PollConfig.IDField); ok && arrayLen > 1 {
		return nil, &clierrors.CLIError{
			Code:     "batch_not_supported",
			Message:  fmt.Sprintf("--wait does not support batch operations (got %d resources)", arrayLen),
			Hint:     "Poll each resource individually with the corresponding get command",
			ExitCode: clierrors.ExitGeneral,
		}
	}

	pathParams, err := statusPathParams(spec.PollConfig.StatusEndpoint, resourceID)
	if err != nil {
		return nil, err
	}

	statusSpec := buildStatusSpec(spec.PollConfig.StatusEndpoint)
	statusInv := &command.Invocation{
		PathParams:  pathParams,
		QueryParams: make(url.Values),
	}
	pollBackoff := RetryConfig{
		BaseDelay: opts.BaseDelay,
		MaxDelay:  opts.MaxDelay,
	}
	start := time.Now()
	var lastStatusResp json.RawMessage

	for attempt := 0; ; attempt++ {
		if err := pollCtx.Err(); err != nil {
			return nil, classifyPollContextError(err, resourceID, lastStatusResp)
		}

		statusResp, err := c.executeWithContext(pollCtx, statusSpec, statusInv)
		if err != nil {
			if ctxErr := pollCtx.Err(); ctxErr != nil {
				return nil, classifyPollContextError(ctxErr, resourceID, lastStatusResp)
			}
			return nil, err
		}
		lastStatusResp = statusResp

		status, err := extractJSONPath(statusResp, spec.PollConfig.StatusField)
		if err != nil {
			return nil, clierrors.New(fmt.Sprintf(
				"failed to extract status from %q: %v",
				spec.PollConfig.StatusField, err,
			))
		}

		if slices.Contains(spec.PollConfig.TerminalOK, status) {
			return statusResp, nil
		}
		if slices.Contains(spec.PollConfig.TerminalFail, status) {
			return nil, &ErrPollFailed{Data: statusResp, Status: status}
		}
		if opts.OnStatus != nil {
			opts.OnStatus(status, time.Since(start))
		}

		delay := backoffDelay(attempt, pollBackoff)
		if err := waitForRetry(pollCtx, delay); err != nil {
			if ctxErr := pollCtx.Err(); ctxErr != nil {
				return nil, classifyPollContextError(ctxErr, resourceID, lastStatusResp)
			}
			return nil, err
		}
	}
}
// Execute sends an HTTP request described by the Spec (static metadata)
// and Invocation (resolved user values). Returns the raw JSON response.
//
// The Spec provides the endpoint template, HTTP method, body encoding,
// and behavioral flags (pagination, polling). The Invocation provides
// the concrete path params, query params, body, and file path.
func (c *Client) Execute(spec *command.Spec, inv *command.Invocation) (json.RawMessage, error) {
	return c.executeWithContext(context.Background(), spec, inv)
}

func (c *Client) executeWithContext(ctx context.Context, spec *command.Spec, inv *command.Invocation) (json.RawMessage, error) {
	if spec.Method == "" {
		return nil, clierrors.New("Spec.Method must be set")
	}

	// Build URL from endpoint template + path params + query params
	reqURL, err := buildURL(c.baseURL, spec.Endpoint, inv.PathParams, inv.QueryParams)
	if err != nil {
		return nil, clierrors.New(fmt.Sprintf("failed to build request URL: %v", err))
	}

	// Build request based on body encoding
	var req *http.Request
	switch spec.BodyEncoding {
	case "multipart":
		req, err = buildMultipartRequest(spec.Method, reqURL, inv.FilePath)
	default:
		req, err = buildJSONRequest(spec.Method, reqURL, inv.Body)
	}
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)

	resp, err := c.Do(req)
	if err != nil {
		return nil, &clierrors.CLIError{
			Code:     "network_error",
			Message:  fmt.Sprintf("request failed: %v", err),
			ExitCode: clierrors.ExitGeneral,
		}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, clierrors.New(fmt.Sprintf("failed to read response body: %v", err))
	}

	if resp.StatusCode >= 400 {
		return nil, parseErrorResponse(resp.StatusCode, respBody, resp.Header.Get("X-Request-Id"))
	}

	return json.RawMessage(respBody), nil
}

// buildURL constructs the full URL with path param substitution and query params.
func buildURL(base, endpoint string, pathParams map[string]string, queryParams url.Values) (string, error) {
	path := endpoint
	for key, val := range pathParams {
		path = strings.ReplaceAll(path, "{"+key+"}", url.PathEscape(val))
	}

	u, err := url.Parse(base + path)
	if err != nil {
		return "", err
	}

	if len(queryParams) > 0 {
		q := u.Query()
		for key, vals := range queryParams {
			for _, v := range vals {
				q.Add(key, v)
			}
		}
		u.RawQuery = q.Encode()
	}

	return u.String(), nil
}

// buildJSONRequest creates an HTTP request with an optional JSON body.
// If body is nil, no request body is sent (used by GET, DELETE, and bodyless POST endpoints).
func buildJSONRequest(method, reqURL string, body map[string]any) (*http.Request, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, clierrors.New(fmt.Sprintf("failed to marshal request body: %v", err))
		}
		reader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, reqURL, reader)
	if err != nil {
		return nil, clierrors.New(fmt.Sprintf("failed to create request: %v", err))
	}
	return req, nil
}

// buildMultipartRequest creates an HTTP request with a multipart/form-data body
// containing the file at the given path. The file is sent under the field name "file".
func buildMultipartRequest(method, reqURL, filePath string) (*http.Request, error) {
	if filePath == "" {
		return nil, clierrors.New("file path is required for multipart upload")
	}

	file, err := os.Open(filePath)
	if err != nil {
		return nil, clierrors.New(fmt.Sprintf("failed to open file %q: %v", filePath, err))
	}
	defer file.Close()

	var requestBody bytes.Buffer
	multipartWriter := multipart.NewWriter(&requestBody)

	filePart, err := multipartWriter.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		return nil, clierrors.New(fmt.Sprintf("failed to create form file: %v", err))
	}

	if _, err := io.Copy(filePart, file); err != nil {
		return nil, clierrors.New(fmt.Sprintf("failed to write file to form: %v", err))
	}

	if err := multipartWriter.Close(); err != nil {
		return nil, clierrors.New(fmt.Sprintf("failed to close multipart writer: %v", err))
	}

	req, err := http.NewRequest(method, reqURL, &requestBody)
	if err != nil {
		return nil, clierrors.New(fmt.Sprintf("failed to create request: %v", err))
	}
	req.Header.Set("Content-Type", multipartWriter.FormDataContentType())

	return req, nil
}

func defaultPollOptions(opts PollOptions) PollOptions {
	if opts.Timeout <= 0 {
		opts.Timeout = 10 * time.Minute
	}
	if opts.BaseDelay <= 0 {
		opts.BaseDelay = 2 * time.Second
	}
	if opts.MaxDelay <= 0 {
		opts.MaxDelay = 30 * time.Second
	}
	return opts
}

func ensurePollContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); ok || timeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, timeout)
}

func buildStatusSpec(endpoint string) *command.Spec {
	return &command.Spec{
		Endpoint: endpoint,
		Method:   http.MethodGet,
	}
}

func statusPathParams(endpoint, resourceID string) (map[string]string, error) {
	params := make(map[string]string)
	start := strings.Index(endpoint, "{")
	end := strings.Index(endpoint, "}")
	if start == -1 || end == -1 || end <= start+1 {
		return nil, clierrors.New(fmt.Sprintf("status endpoint %q must contain exactly one path parameter", endpoint))
	}
	if strings.Contains(endpoint[end+1:], "{") {
		return nil, clierrors.New(fmt.Sprintf("status endpoint %q must contain exactly one path parameter", endpoint))
	}
	params[endpoint[start+1:end]] = resourceID
	return params, nil
}

// extractArrayLen checks if a dot-notation path ends with a numeric index
// (e.g., "data.ids.0"), and if so, returns the length of that array.
// Returns (0, false) if the path doesn't end with an array index.
func extractArrayLen(raw json.RawMessage, path string) (int, bool) {
	parts := strings.Split(path, ".")
	if len(parts) < 2 {
		return 0, false
	}
	// Check if the last segment is a numeric index
	if _, err := strconv.Atoi(parts[len(parts)-1]); err != nil {
		return 0, false
	}
	// Walk to the parent array
	arrayPath := strings.Join(parts[:len(parts)-1], ".")

	var current any
	if err := json.Unmarshal(raw, &current); err != nil {
		return 0, false
	}
	for _, part := range strings.Split(arrayPath, ".") {
		obj, ok := current.(map[string]any)
		if !ok {
			return 0, false
		}
		next, ok := obj[part]
		if !ok {
			return 0, false
		}
		current = next
	}
	arr, ok := current.([]any)
	if !ok {
		return 0, false
	}
	return len(arr), true
}

// extractJSONPath extracts a string value from JSON using dot-notation.
// Supports object fields ("data.status") and array indices ("data.ids.0").
func extractJSONPath(raw json.RawMessage, path string) (string, error) {
	if path == "" {
		return "", clierrors.New("JSON path is required")
	}

	var current any
	if err := json.Unmarshal(raw, &current); err != nil {
		return "", clierrors.New(fmt.Sprintf("failed to parse JSON response: %v", err))
	}

	for _, part := range strings.Split(path, ".") {
		// Try numeric index for arrays
		if idx, err := strconv.Atoi(part); err == nil {
			arr, ok := current.([]any)
			if !ok {
				return "", clierrors.New(fmt.Sprintf("field at %q is not an array", part))
			}
			if idx < 0 || idx >= len(arr) {
				return "", clierrors.New(fmt.Sprintf("array index %d out of bounds (length %d)", idx, len(arr)))
			}
			current = arr[idx]
			continue
		}

		obj, ok := current.(map[string]any)
		if !ok {
			return "", clierrors.New(fmt.Sprintf("field %q is not an object", part))
		}

		next, ok := obj[part]
		if !ok {
			return "", clierrors.New(fmt.Sprintf("field %q not found", part))
		}
		current = next
	}

	value, ok := current.(string)
	if !ok || value == "" {
		return "", clierrors.New(fmt.Sprintf("field %q is not a string", path))
	}
	return value, nil
}

// translateCreateContextError converts timeout/cancel errors before a resource
// ID is known into user-friendly CLIErrors. At this stage the client cannot
// confirm whether creation succeeded, so exit code stays general.
func translateCreateContextError(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return newCreateContextError(ctxErr)
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return newCreateContextError(err)
	}

	var cliErr *clierrors.CLIError
	if errors.As(err, &cliErr) && cliErr.Code == "network_error" {
		msg := strings.ToLower(cliErr.Message)
		switch {
		case strings.Contains(msg, "context deadline exceeded"):
			return newCreateContextError(context.DeadlineExceeded)
		case strings.Contains(msg, "context canceled"), strings.Contains(msg, "context cancelled"):
			return newCreateContextError(context.Canceled)
		}
	}

	return err
}

func newCreateContextError(err error) error {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return &clierrors.CLIError{
			Code:     "timeout",
			Message:  "polling timed out before the operation completed",
			Hint:     "Re-run the corresponding get command to check the current status manually",
			ExitCode: clierrors.ExitTimeout,
		}
	case errors.Is(err, context.Canceled):
		return &clierrors.CLIError{
			Code:     "canceled",
			Message:  "polling was canceled before the operation completed",
			Hint:     "Re-run the corresponding get command to check the current status manually",
			ExitCode: clierrors.ExitGeneral,
		}
	default:
		return clierrors.New(err.Error())
	}
}

func classifyPollContextError(err error, resourceID string, lastResp json.RawMessage) error {
	if err == nil {
		return clierrors.New("unexpected nil context error during polling")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &ErrPollTimeout{
			Data:       lastResp,
			ResourceID: resourceID,
		}
	}
	return &clierrors.CLIError{
		Code:     "canceled",
		Message:  "polling was canceled before the operation completed",
		Hint:     "Re-run the corresponding get command to check the current status manually",
		ExitCode: clierrors.ExitGeneral,
	}
}

// parseErrorResponse parses an API error response into a CLIError.
func parseErrorResponse(statusCode int, body []byte, requestID string) *clierrors.CLIError {
	var envelope struct {
		Error clierrors.APIError `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil && envelope.Error.Message != "" {
		return clierrors.FromAPIError(statusCode, &envelope.Error, requestID)
	}

	return &clierrors.CLIError{
		Code:      "error",
		Message:   fmt.Sprintf("API returned HTTP %d", statusCode),
		RequestID: requestID,
		ExitCode:  clierrors.ExitGeneral,
	}
}
