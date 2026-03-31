package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/heygen-com/heygen-cli/internal/command"
	clierrors "github.com/heygen-com/heygen-cli/internal/errors"
)

// Execute sends an HTTP request described by the Spec (static metadata)
// and Invocation (resolved user values). Returns the raw JSON response.
//
// The Spec provides the endpoint template, HTTP method, body encoding,
// and behavioral flags (pagination, polling). The Invocation provides
// the concrete path params, query params, body, and file path.
func (c *Client) Execute(spec *command.Spec, inv *command.Invocation) (json.RawMessage, error) {
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
