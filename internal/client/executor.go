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

const paginationHardLimit = 10000

// ErrPaginationTruncated is returned when ExecuteAll stops early at the hard
// item limit. It carries the partial data so callers can still render it.
type ErrPaginationTruncated struct {
	Data  json.RawMessage
	Count int
}

func (e *ErrPaginationTruncated) Error() string {
	return fmt.Sprintf("pagination stopped at %d items (hard limit); results may be incomplete", e.Count)
}

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

// ExecuteAll fetches all pages of a paginated endpoint and returns a flat JSON
// array containing all accumulated items.
func (c *Client) ExecuteAll(spec *command.Spec, inv *command.Invocation) (json.RawMessage, error) {
	if !spec.Paginated || spec.TokenField == "" || spec.TokenParam == "" || spec.DataField == "" {
		return nil, clierrors.New("spec is not configured for pagination")
	}

	workingInv := cloneInvocation(inv)
	accumulated := make([]json.RawMessage, 0)

	for {
		page, err := c.Execute(spec, workingInv)
		if err != nil {
			return nil, err
		}

		items, nextToken, err := extractPage(page, spec.DataField, spec.TokenField)
		if err != nil {
			return nil, err
		}

		remaining := paginationHardLimit - len(accumulated)
		if remaining <= 0 {
			data, err := marshalItems(accumulated)
			if err != nil {
				return nil, err
			}
			return nil, &ErrPaginationTruncated{Data: data, Count: len(accumulated)}
		}

		if len(items) > remaining {
			items = items[:remaining]
		}
		accumulated = append(accumulated, items...)

		if nextToken == "" {
			return marshalItems(accumulated)
		}
		if len(accumulated) >= paginationHardLimit {
			data, err := marshalItems(accumulated)
			if err != nil {
				return nil, err
			}
			return nil, &ErrPaginationTruncated{Data: data, Count: len(accumulated)}
		}

		workingInv.QueryParams.Set(spec.TokenParam, nextToken)
	}
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

func cloneInvocation(inv *command.Invocation) *command.Invocation {
	cloned := &command.Invocation{
		PathParams:  make(map[string]string, len(inv.PathParams)),
		QueryParams: make(url.Values, len(inv.QueryParams)),
		FilePath:    inv.FilePath,
	}

	for key, value := range inv.PathParams {
		cloned.PathParams[key] = value
	}
	for key, values := range inv.QueryParams {
		copied := make([]string, len(values))
		copy(copied, values)
		cloned.QueryParams[key] = copied
	}
	if inv.Body != nil {
		// Shallow copy — nested maps/slices are shared. Safe because pagination
		// only mutates QueryParams, never Body values.
		cloned.Body = make(map[string]any, len(inv.Body))
		for key, value := range inv.Body {
			cloned.Body[key] = value
		}
	}

	return cloned
}

func extractPage(raw json.RawMessage, dataField, tokenField string) ([]json.RawMessage, string, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, "", clierrors.New(fmt.Sprintf("failed to parse paginated response: %v", err))
	}

	dataRaw, ok := envelope[dataField]
	if !ok {
		return nil, "", clierrors.New(fmt.Sprintf("response missing %q field", dataField))
	}

	var items []json.RawMessage
	if err := json.Unmarshal(dataRaw, &items); err != nil {
		return nil, "", clierrors.New(fmt.Sprintf("response field %q is not an array: %v", dataField, err))
	}

	tokenRaw, ok := envelope[tokenField]
	if !ok {
		return items, "", nil
	}
	if string(tokenRaw) == "null" {
		return items, "", nil
	}

	var token string
	if err := json.Unmarshal(tokenRaw, &token); err != nil {
		return nil, "", clierrors.New(fmt.Sprintf("response field %q is not a string: %v", tokenField, err))
	}
	return items, token, nil
}

func marshalItems(items []json.RawMessage) (json.RawMessage, error) {
	data, err := json.Marshal(items)
	if err != nil {
		return nil, clierrors.New(fmt.Sprintf("failed to marshal paginated results: %v", err))
	}
	return json.RawMessage(data), nil
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
