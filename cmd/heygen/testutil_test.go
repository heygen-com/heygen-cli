package main

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	clierrors "github.com/heygen-com/heygen-cli/internal/errors"
	"github.com/heygen-com/heygen-cli/internal/output"
)

// testHandler is a handler for the mock API server.
type testHandler struct {
	StatusCode int
	Body       string
	Headers    map[string]string
	// ValidateRequest allows tests to inspect the incoming request.
	ValidateRequest func(t *testing.T, r *http.Request)
}

// cmdResult captures the output of a CLI command execution.
type cmdResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// setupTestServer creates an httptest.Server with registered handlers.
// handlers maps "METHOD /path" → testHandler.
func setupTestServer(t *testing.T, handlers map[string]testHandler) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path
		h, ok := handlers[key]
		if !ok {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error":{"code":"not_found","message":"no handler registered"}}`))
			return
		}
		if h.ValidateRequest != nil {
			h.ValidateRequest(t, r)
		}
		for k, v := range h.Headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(h.StatusCode)
		w.Write([]byte(h.Body))
	}))
}

// runCommand executes a CLI command against a test server, mirroring the
// production error-rendering path from main().
//
// It creates a fresh Cobra command tree with a JSONFormatter backed by
// buffers, executes the command, and renders returned errors through the
// formatter — same path as main(). This ensures stderr content in tests
// matches what production emits.
func runCommand(t *testing.T, serverURL, apiKey string, args ...string) cmdResult {
	t.Helper()

	var stdout, stderr bytes.Buffer
	formatter := output.NewJSONFormatter(&stdout, &stderr)

	// Set env vars for this test
	t.Setenv("HEYGEN_API_KEY", apiKey)
	t.Setenv("HEYGEN_API_BASE", serverURL)

	cmd := newRootCmd("test", formatter)
	cmd.SetArgs(args)

	err := cmd.Execute()

	var exitCode int
	if err != nil {
		// Render through formatter — same path as main()
		var cliErr *clierrors.CLIError
		if errors.As(err, &cliErr) {
			formatter.Error(cliErr)
			exitCode = cliErr.ExitCode
		} else {
			wrapped := clierrors.New(err.Error())
			formatter.Error(wrapped)
			exitCode = wrapped.ExitCode
		}
	}

	return cmdResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
	}
}
