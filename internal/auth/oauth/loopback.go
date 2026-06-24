package oauth

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"html"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ErrInvalidRedirectPath is returned by StartLoopbackServer when the
// supplied redirect path is malformed (does not start with '/', contains
// '?' or '#', etc.). Surfacing a typed error makes the misuse obvious to
// callers and pinable in tests.
var ErrInvalidRedirectPath = errors.New("oauth: invalid redirect path")

// DefaultLoopbackTimeout is how long StartLoopbackServer waits for the
// browser to hit the redirect URI before giving up.
const DefaultLoopbackTimeout = 5 * time.Minute

// LoopbackResult carries the OAuth callback parameters captured by the
// loopback server. Either Code is populated (success) or Err is
// populated (state mismatch, IdP error, timeout, etc.) — never both.
type LoopbackResult struct {
	Code        string
	State       string
	RedirectURI string
	Err         error
}

// LoopbackServer is a one-shot HTTP server bound to 127.0.0.1 on an
// ephemeral port. It serves a single GET on the configured redirect
// path, captures the OAuth callback parameters, renders a small
// "you can close this tab" page, and shuts down.
type LoopbackServer struct {
	Port          int
	RedirectURI   string
	ExpectedState string

	results   chan LoopbackResult
	server    *http.Server
	once      sync.Once
	done      chan struct{}
	deliverMu sync.Mutex
	delivered bool
}

// StartLoopbackServer binds 127.0.0.1:0, serves redirectPath on a single
// GET, and returns the captured code via the result channel. The
// returned cancel function tears the server down; it is safe to call
// more than once.
//
// The supplied context governs the lifetime of the server: cancelling
// it (or the 5-minute timeout firing) closes the listener and emits an
// error result. Pass the expected `state` value generated alongside the
// PKCE pair — the server rejects mismatched callbacks with a constant-
// time comparison.
func StartLoopbackServer(
	ctx context.Context,
	redirectPath, expectedState string,
) (*LoopbackServer, <-chan LoopbackResult, func(), error) {
	if redirectPath == "" || redirectPath[0] != '/' {
		return nil, nil, nil, fmt.Errorf("%w: must start with '/', got %q", ErrInvalidRedirectPath, redirectPath)
	}
	// `?` and `#` would silently register a dead route — net/http's mux
	// matches only on the path component, so a "callback?source=cli" path
	// never receives the IdP's bare `/callback` redirect. Reject early so
	// the misuse surfaces at boot, not as a timeout.
	if strings.ContainsAny(redirectPath, "?#") {
		return nil, nil, nil, fmt.Errorf("%w: must not contain '?' or '#', got %q", ErrInvalidRedirectPath, redirectPath)
	}
	if expectedState == "" {
		return nil, nil, nil, errors.New("oauth: expectedState must be non-empty")
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, nil, nil, fmt.Errorf("oauth: bind loopback listener: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d%s", port, redirectPath)

	results := make(chan LoopbackResult, 1)
	lb := &LoopbackServer{
		Port:          port,
		RedirectURI:   redirectURI,
		ExpectedState: expectedState,
		results:       results,
		done:          make(chan struct{}),
	}

	mux := http.NewServeMux()
	mux.HandleFunc(redirectPath, lb.handleCallback)

	lb.server = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		// Serve returns ErrServerClosed on graceful shutdown — not an
		// error worth surfacing. Any other error is propagated to the
		// caller via the results channel AND the watchdog is signalled
		// to exit so we don't leak a goroutine for the full 5-min
		// DefaultLoopbackTimeout window.
		err := lb.server.Serve(listener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			lb.deliver(LoopbackResult{Err: fmt.Errorf("oauth: loopback server: %w", err)})
			lb.shutdown()
		}
	}()

	// Wire up timeout + context cancellation. The driver-level timeout is
	// belt-and-braces alongside the caller-supplied context, so the
	// browser-side hang never wedges a CLI shell longer than ~5 min.
	timer := time.NewTimer(DefaultLoopbackTimeout)
	go func() {
		select {
		case <-ctx.Done():
			timer.Stop()
			lb.deliver(LoopbackResult{Err: fmt.Errorf("oauth: loopback cancelled: %w", ctx.Err())})
			lb.shutdown()
		case <-timer.C:
			lb.deliver(LoopbackResult{Err: fmt.Errorf("oauth: loopback timed out after %s", DefaultLoopbackTimeout)})
			lb.shutdown()
		case <-lb.done:
			timer.Stop()
		}
	}()

	cancel := func() {
		timer.Stop()
		lb.shutdown()
	}
	return lb, results, cancel, nil
}

// deliver sends a result on the channel exactly once, dropping later
// calls so a stray callback can't double-deliver.
func (lb *LoopbackServer) deliver(r LoopbackResult) {
	lb.deliverMu.Lock()
	defer lb.deliverMu.Unlock()
	if lb.delivered {
		return
	}
	lb.delivered = true
	select {
	case lb.results <- r:
	default:
	}
}

// shutdown closes the server exactly once. The results channel is
// deliberately left open: any goroutine that already passed the
// `delivered` guard but hasn't yet sent would panic on a close, so the
// channel is allowed to be garbage-collected with the LoopbackServer.
func (lb *LoopbackServer) shutdown() {
	lb.once.Do(func() {
		close(lb.done)
		// Best-effort close; the listener may already be torn down. Use
		// a short timeout so we don't hang on idle keep-alive sockets
		// (browsers default to keep-alive and Chrome holds them open
		// for minutes).
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = lb.server.Shutdown(shutdownCtx)
		// Close existing connections that survived Shutdown's grace
		// period (keep-alive sockets). Without this the CLI process can
		// hang for minutes waiting for a browser to release its idle
		// socket.
		_ = lb.server.Close()
	})
}

// handleCallback serves the OAuth redirect. Only GET requests on the
// expected path are honored; anything else gets a 404 without revealing
// that a CLI is listening.
func (lb *LoopbackServer) handleCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	q := r.URL.Query()
	if oauthErr := q.Get("error"); oauthErr != "" {
		desc := q.Get("error_description")
		writeErrorPage(w, http.StatusBadRequest, oauthErr, desc)
		err := fmt.Errorf("oauth: authorize returned error: %s", oauthErr)
		if desc != "" {
			err = fmt.Errorf("%w — %s", err, desc)
		}
		lb.deliver(LoopbackResult{Err: err, RedirectURI: lb.RedirectURI})
		go lb.shutdown()
		return
	}

	state := q.Get("state")
	if state == "" || subtle.ConstantTimeCompare([]byte(state), []byte(lb.ExpectedState)) != 1 {
		writeErrorPage(w, http.StatusBadRequest, "invalid_state", "State parameter did not match.")
		lb.deliver(LoopbackResult{Err: errors.New("oauth: state mismatch — possible CSRF, aborting"), RedirectURI: lb.RedirectURI})
		go lb.shutdown()
		return
	}

	code := q.Get("code")
	if code == "" {
		writeErrorPage(w, http.StatusBadRequest, "missing_code", "Authorization code is missing from the redirect.")
		lb.deliver(LoopbackResult{Err: errors.New("oauth: redirect did not include `code`"), RedirectURI: lb.RedirectURI})
		go lb.shutdown()
		return
	}

	writeSuccessPage(w)
	lb.deliver(LoopbackResult{Code: code, State: state, RedirectURI: lb.RedirectURI})
	go lb.shutdown()
}

func writeSuccessPage(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	// Browsers default to keep-alive; without an explicit close hint
	// Shutdown waits on the idle socket and the CLI lingers for minutes.
	w.Header().Set("Connection", "close")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(successPageHTML))
}

func writeErrorPage(w http.ResponseWriter, status int, code, description string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "close")
	w.WriteHeader(status)
	body := fmt.Sprintf(errorPageHTMLTemplate, html.EscapeString(code), html.EscapeString(description))
	_, _ = w.Write([]byte(body))
}

const successPageHTML = `<!doctype html><html><head><meta charset="utf-8"><title>Signed in to HeyGen</title>
<style>body{font-family:system-ui,sans-serif;display:flex;align-items:center;justify-content:center;height:100vh;margin:0;background:#0b0f14;color:#e6e8eb}main{max-width:480px;text-align:center;padding:32px;border-radius:12px;background:#11161d;border:1px solid #1f2630}h1{font-weight:600;margin:0 0 8px;color:#3CE6AC}p{margin:0;color:#9aa3ad}</style>
</head><body><main><h1>You're signed in.</h1><p>You can close this tab and return to your terminal.</p></main></body></html>`

const errorPageHTMLTemplate = `<!doctype html><html><head><meta charset="utf-8"><title>Sign-in failed</title>
<style>body{font-family:system-ui,sans-serif;display:flex;align-items:center;justify-content:center;height:100vh;margin:0;background:#1a0b0b;color:#e6e8eb}main{max-width:480px;text-align:center;padding:32px;border-radius:12px;background:#1d1111;border:1px solid #301f1f}h1{font-weight:600;margin:0 0 8px;color:#ff7a7a}code{background:#2a1414;padding:2px 6px;border-radius:4px}p{margin:8px 0 0;color:#9aa3ad}</style>
</head><body><main><h1>Sign-in failed</h1><p><code>%s</code></p><p>%s</p></main></body></html>`
