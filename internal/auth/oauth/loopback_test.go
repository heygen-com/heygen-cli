package oauth

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestStartLoopbackServer_HappyPath(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	lb, results, stop, err := StartLoopbackServer(ctx, "/oauth/callback", "the-state")
	if err != nil {
		t.Fatalf("StartLoopbackServer: %v", err)
	}
	defer stop()

	if lb.Port == 0 {
		t.Error("Port should be assigned")
	}
	if !strings.HasPrefix(lb.RedirectURI, "http://127.0.0.1:") {
		t.Errorf("RedirectURI = %q", lb.RedirectURI)
	}

	cbURL := lb.RedirectURI + "?" + url.Values{
		"code":  {"the-code"},
		"state": {"the-state"},
	}.Encode()
	resp, err := http.Get(cbURL) //nolint:noctx // test client
	if err != nil {
		t.Fatalf("GET callback: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, body=%s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "Signed in") {
		t.Errorf("expected success page, got %s", body)
	}

	select {
	case r := <-results:
		if r.Err != nil {
			t.Fatalf("unexpected Err: %v", r.Err)
		}
		if r.Code != "the-code" {
			t.Errorf("Code = %q", r.Code)
		}
		if r.State != "the-state" {
			t.Errorf("State = %q", r.State)
		}
		if r.RedirectURI != lb.RedirectURI {
			t.Errorf("RedirectURI = %q, want %q", r.RedirectURI, lb.RedirectURI)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for result")
	}
}

func TestStartLoopbackServer_StateMismatchRejects(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	lb, results, stop, err := StartLoopbackServer(ctx, "/oauth/callback", "expected-state")
	if err != nil {
		t.Fatalf("StartLoopbackServer: %v", err)
	}
	defer stop()

	cbURL := lb.RedirectURI + "?" + url.Values{
		"code":  {"the-code"},
		"state": {"different-state"},
	}.Encode()
	resp, err := http.Get(cbURL) //nolint:noctx // test client
	if err != nil {
		t.Fatalf("GET callback: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}

	select {
	case r := <-results:
		if r.Err == nil {
			t.Fatal("expected error result")
		}
		if !strings.Contains(r.Err.Error(), "state mismatch") {
			t.Errorf("err = %v, want state-mismatch message", r.Err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for result")
	}
}

func TestStartLoopbackServer_PropagatesIdPError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	lb, results, stop, err := StartLoopbackServer(ctx, "/oauth/callback", "the-state")
	if err != nil {
		t.Fatalf("StartLoopbackServer: %v", err)
	}
	defer stop()

	cbURL := lb.RedirectURI + "?" + url.Values{
		"error":             {"access_denied"},
		"error_description": {"user said no"},
		"state":             {"the-state"},
	}.Encode()
	resp, err := http.Get(cbURL) //nolint:noctx // test client
	if err != nil {
		t.Fatalf("GET callback: %v", err)
	}
	_ = resp.Body.Close()

	select {
	case r := <-results:
		if r.Err == nil {
			t.Fatal("expected error result")
		}
		if !strings.Contains(r.Err.Error(), "access_denied") {
			t.Errorf("err = %v, want to include error code", r.Err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for result")
	}
}

func TestStartLoopbackServer_MissingCodeRejects(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	lb, results, stop, err := StartLoopbackServer(ctx, "/oauth/callback", "the-state")
	if err != nil {
		t.Fatalf("StartLoopbackServer: %v", err)
	}
	defer stop()

	cbURL := lb.RedirectURI + "?state=the-state"
	resp, err := http.Get(cbURL) //nolint:noctx // test client
	if err != nil {
		t.Fatalf("GET callback: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}

	select {
	case r := <-results:
		if r.Err == nil {
			t.Fatal("expected error result")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for result")
	}
}

func TestStartLoopbackServer_ContextCancelDelivers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	_, results, stop, err := StartLoopbackServer(ctx, "/oauth/callback", "the-state")
	if err != nil {
		t.Fatalf("StartLoopbackServer: %v", err)
	}
	defer stop()

	cancel()

	select {
	case r := <-results:
		if r.Err == nil {
			t.Fatal("expected error result on cancel")
		}
		if !errors.Is(r.Err, context.Canceled) {
			t.Errorf("want context.Canceled in chain, got %v", r.Err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for cancel result")
	}
}

func TestStartLoopbackServer_RejectsNonGet(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	lb, _, stop, err := StartLoopbackServer(ctx, "/oauth/callback", "the-state")
	if err != nil {
		t.Fatalf("StartLoopbackServer: %v", err)
	}
	defer stop()

	resp, err := http.Post(lb.RedirectURI, "text/plain", strings.NewReader("nope")) //nolint:noctx // test client
	if err != nil {
		t.Fatalf("POST callback: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", resp.StatusCode)
	}
}

func TestStartLoopbackServer_ValidatesArgs(t *testing.T) {
	tests := []struct {
		name, path, state string
		wantSentinel      bool
	}{
		{"empty path", "", "s", true},
		{"path missing slash", "callback", "s", true},
		{"path with question mark", "/cb?source=cli", "s", true},
		{"path with fragment", "/cb#frag", "s", true},
		{"empty state", "/cb", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, err := StartLoopbackServer(context.Background(), tc.path, tc.state)
			if err == nil {
				t.Fatal("expected error")
			}
			if tc.wantSentinel && !errors.Is(err, ErrInvalidRedirectPath) {
				t.Errorf("expected ErrInvalidRedirectPath, got %v", err)
			}
			if !tc.wantSentinel && errors.Is(err, ErrInvalidRedirectPath) {
				t.Errorf("did not expect ErrInvalidRedirectPath, got %v", err)
			}
		})
	}
}

// Every callback-reachable error result must wrap a sentinel: the CLI
// maps them to distinct analytics reasons with errors.Is, so an
// unwrapped error silently collapses back into the catch-all bucket.
// Non-callback deliveries live elsewhere: the timer in
// TestStartLoopbackServer_TimerDeliversTimeoutSentinel, cancellation in
// TestStartLoopbackServer_ContextCancelDelivers.
func TestStartLoopbackServer_ErrorsWrapSentinels(t *testing.T) {
	tests := []struct {
		name  string
		query url.Values
		want  error
	}{
		{
			name:  "user declined",
			query: url.Values{"error": {"access_denied"}, "error_description": {"user said no"}, "state": {"the-state"}},
			want:  ErrAuthorizeDenied,
		},
		{
			name:  "other IdP error",
			query: url.Values{"error": {"invalid_scope"}, "state": {"the-state"}},
			want:  ErrAuthorizeFailed,
		},
		{
			name:  "error response with wrong state is a state mismatch, not a denial",
			query: url.Values{"error": {"access_denied"}, "state": {"attacker-state"}},
			want:  ErrStateMismatch,
		},
		{
			name:  "error response with no state is a state mismatch, not a denial",
			query: url.Values{"error": {"access_denied"}},
			want:  ErrStateMismatch,
		},
		{
			name:  "state mismatch",
			query: url.Values{"code": {"the-code"}, "state": {"wrong-state"}},
			want:  ErrStateMismatch,
		},
		{
			name:  "missing code",
			query: url.Values{"state": {"the-state"}},
			want:  ErrMissingCode,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			lb, results, stop, err := StartLoopbackServer(ctx, "/oauth/callback", "the-state")
			if err != nil {
				t.Fatalf("StartLoopbackServer: %v", err)
			}
			defer stop()

			resp, err := http.Get(lb.RedirectURI + "?" + tt.query.Encode()) //nolint:noctx // test client
			if err != nil {
				t.Fatalf("GET callback: %v", err)
			}
			_ = resp.Body.Close()

			select {
			case r := <-results:
				if !errors.Is(r.Err, tt.want) {
					t.Errorf("err = %v, want wrapping %v", r.Err, tt.want)
				}
			case <-time.After(time.Second):
				t.Fatal("timed out waiting for result")
			}
		})
	}
}

// access_denied must not fall into ErrAuthorizeFailed: the two are split
// so telemetry can separate a user declining (expected) from an IdP
// misconfiguration (our bug).
func TestStartLoopbackServer_DeniedIsNotGenericAuthorizeFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	lb, results, stop, err := StartLoopbackServer(ctx, "/oauth/callback", "the-state")
	if err != nil {
		t.Fatalf("StartLoopbackServer: %v", err)
	}
	defer stop()

	resp, err := http.Get(lb.RedirectURI + "?error=access_denied&state=the-state") //nolint:noctx // test client
	if err != nil {
		t.Fatalf("GET callback: %v", err)
	}
	_ = resp.Body.Close()

	select {
	case r := <-results:
		if !errors.Is(r.Err, ErrAuthorizeDenied) {
			t.Fatalf("err = %v, want ErrAuthorizeDenied", r.Err)
		}
		if errors.Is(r.Err, ErrAuthorizeFailed) {
			t.Errorf("access_denied must not match ErrAuthorizeFailed, got %v", r.Err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for result")
	}
}

// The timer path is what produced ~68% of production login failures, so
// it gets a real test via the unexported timeout seam rather than a
// five-minute wait. Covers the seam's timer branch, not the exported
// wrapper's choice of duration.
func TestStartLoopbackServer_TimerDeliversTimeoutSentinel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, results, stop, err := startLoopbackServer(ctx, "/oauth/callback", "the-state", 50*time.Millisecond)
	if err != nil {
		t.Fatalf("startLoopbackServer: %v", err)
	}
	defer stop()

	select {
	case r := <-results:
		if !errors.Is(r.Err, ErrLoopbackTimeout) {
			t.Errorf("err = %v, want ErrLoopbackTimeout", r.Err)
		}
		// Must NOT look like cancellation: the two map to different
		// analytics reasons and the caller distinguishes them.
		if errors.Is(r.Err, ErrLoopbackCanceled) {
			t.Errorf("timer result must not match ErrLoopbackCanceled, got %v", r.Err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for timer result")
	}
}

// Pins the shipped timeout value only. It does NOT prove the exported
// wrapper still forwards it — the timer is not observable without
// waiting it out — so treat this as a guard against the constant
// drifting, not against the wiring changing.
func TestDefaultLoopbackTimeout_Unchanged(t *testing.T) {
	if DefaultLoopbackTimeout != 5*time.Minute {
		t.Errorf("DefaultLoopbackTimeout = %v, want 5m", DefaultLoopbackTimeout)
	}
}
