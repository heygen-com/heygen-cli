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
		if err != nil {
			return nil, err
		}
	default:
		var body io.Reader
		if inv.Body != nil {
			data, marshalErr := json.Marshal(inv.Body)
			if marshalErr != nil {
				return nil, clierrors.New(fmt.Sprintf("failed to marshal request body: %v", marshalErr))
			}
			body = bytes.NewReader(data)
		}
		req, err = http.NewRequest(spec.Method, reqURL, body)
		if err != nil {
			return nil, clierrors.New(fmt.Sprintf("failed to create request: %v", err))
		}
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

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	part, err := writer.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		return nil, clierrors.New(fmt.Sprintf("failed to create form file: %v", err))
	}

	if _, err := io.Copy(part, file); err != nil {
		return nil, clierrors.New(fmt.Sprintf("failed to write file to form: %v", err))
	}

	if err := writer.Close(); err != nil {
		return nil, clierrors.New(fmt.Sprintf("failed to close multipart writer: %v", err))
	}

	req, err := http.NewRequest(method, reqURL, &buf)
	if err != nil {
		return nil, clierrors.New(fmt.Sprintf("failed to create request: %v", err))
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

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
