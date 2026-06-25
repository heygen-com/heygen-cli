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
