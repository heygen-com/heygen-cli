package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	clierrors "github.com/heygen-com/heygen-cli/internal/errors"
)

// Execute converts a RequestSpec into an HTTP request, sends it, and returns
// the raw JSON response body. Errors are returned as *CLIError with the
// appropriate exit code and any X-Request-Id from the response.
func (c *Client) Execute(spec RequestSpec) (json.RawMessage, error) {
	if spec.BodyEncoding == "multipart" {
		return nil, clierrors.NewUsage("multipart upload is not yet implemented")
	}

	// Build URL
	reqURL, err := buildURL(c.baseURL, spec.Endpoint, spec.PathParams, spec.QueryParams)
	if err != nil {
		return nil, clierrors.New(fmt.Sprintf("failed to build request URL: %v", err))
	}

	// Build body
	var body io.Reader
	if len(spec.Body) > 0 {
		bodyMap := fieldSpecsToMap(spec.Body)
		data, marshalErr := json.Marshal(bodyMap)
		if marshalErr != nil {
			return nil, clierrors.New(fmt.Sprintf("failed to marshal request body: %v", marshalErr))
		}
		body = bytes.NewReader(data)
	}

	// Create request
	method := spec.Method
	if method == "" {
		method = "GET"
	}
	req, err := http.NewRequest(method, reqURL, body)
	if err != nil {
		return nil, clierrors.New(fmt.Sprintf("failed to create request: %v", err))
	}

	// Execute
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

	// Handle errors
	if resp.StatusCode >= 400 {
		return nil, parseErrorResponse(resp.StatusCode, respBody, resp.Header.Get("X-Request-Id"))
	}

	return json.RawMessage(respBody), nil
}

// buildURL constructs the full URL with path param substitution and query params.
func buildURL(base, endpoint string, pathParams map[string]string, queryParams []QueryParam) (string, error) {
	// Substitute path parameters
	path := endpoint
	for key, val := range pathParams {
		path = strings.ReplaceAll(path, "{"+key+"}", url.PathEscape(val))
	}

	u, err := url.Parse(base + path)
	if err != nil {
		return "", err
	}

	// Add query parameters (supports repeated keys)
	if len(queryParams) > 0 {
		q := u.Query()
		for _, p := range queryParams {
			if p.Repeated {
				q.Add(p.Key, p.Value)
			} else {
				q.Set(p.Key, p.Value)
			}
		}
		u.RawQuery = q.Encode()
	}

	return u.String(), nil
}

// fieldSpecsToMap converts a slice of FieldSpec into a JSON-ready map.
func fieldSpecsToMap(fields []FieldSpec) map[string]any {
	m := make(map[string]any, len(fields))
	for _, f := range fields {
		if f.Value != nil {
			m[f.Name] = f.Value
		}
	}
	return m
}

// parseErrorResponse parses an API error response into a CLIError.
func parseErrorResponse(statusCode int, body []byte, requestID string) *clierrors.CLIError {
	// Try to parse the standard error envelope: {"error": {...}}
	var envelope struct {
		Error clierrors.APIError `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil && envelope.Error.Message != "" {
		return clierrors.FromAPIError(statusCode, &envelope.Error, requestID)
	}

	// Fallback: couldn't parse error envelope
	return &clierrors.CLIError{
		Code:      "error",
		Message:   fmt.Sprintf("API returned HTTP %d", statusCode),
		RequestID: requestID,
		ExitCode:  clierrors.ExitGeneral,
	}
}
