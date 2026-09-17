package main

import (
	"net/http"
	"testing"
)

// Routing is by FlagSpec.Source, not by flag name, so this pins all three hops
// at once: codegen emitting the flag, BuildInvocation filing it under
// Invocation.Headers, and the wire carrying the header name over the flag name.
func TestIdempotencyKeyFlagReachesRequestHeader(t *testing.T) {
	const key = "550e8400-e29b-41d4-a716-446655440000"
	var got string

	srv := setupTestServer(t, map[string]testHandler{
		"POST /v3/videos": {
			StatusCode: http.StatusOK,
			Body:       `{"data":{"id":"v1"}}`,
			ValidateRequest: func(t *testing.T, r *http.Request) {
				got = r.Header.Get("Idempotency-Key")
			},
		},
	})
	defer srv.Close()

	res := runCommand(t, srv.URL, "test-key",
		"video", "create", "-d", `{"type":"avatar"}`, "--idempotency-key", key)

	if res.ExitCode != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", res.ExitCode, res.Stderr)
	}
	if got != key {
		t.Errorf("Idempotency-Key = %q, want %q", got, key)
	}
}

// A command whose spec does not declare the header must not accept the flag,
// which is the property that makes the flag per-operation rather than global.
func TestIdempotencyKeyFlagAbsentWhereSpecOmitsIt(t *testing.T) {
	srv := setupTestServer(t, map[string]testHandler{})
	defer srv.Close()

	res := runCommand(t, srv.URL, "test-key",
		"video", "list", "--idempotency-key", "550e8400-e29b-41d4-a716-446655440000")

	if res.ExitCode != 2 {
		t.Errorf("exit = %d, want 2 (unknown flag is a usage error); stderr: %s",
			res.ExitCode, res.Stderr)
	}
}

// Omitting the flag must send no header at all. The API rejects a blank
// Idempotency-Key with 400, so a header materialized from an unset flag would
// fail every create rather than degrade quietly.
func TestIdempotencyKeyOmittedSendsNoHeader(t *testing.T) {
	var present bool

	srv := setupTestServer(t, map[string]testHandler{
		"POST /v3/videos": {
			StatusCode: http.StatusOK,
			Body:       `{"data":{"id":"v1"}}`,
			ValidateRequest: func(t *testing.T, r *http.Request) {
				_, present = r.Header["Idempotency-Key"]
			},
		},
	})
	defer srv.Close()

	res := runCommand(t, srv.URL, "test-key", "video", "create", "-d", `{"type":"avatar"}`)

	if res.ExitCode != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", res.ExitCode, res.Stderr)
	}
	if present {
		t.Error("Idempotency-Key was sent although the flag was omitted")
	}
}
