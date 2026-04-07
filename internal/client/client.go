package client

import (
	"net/http"
	"time"
)

const (
	DefaultBaseURL   = "https://api.heygen.com"
	DefaultUserAgent = "heygen-cli/dev"
	DefaultTimeout   = 30 * time.Second
)

// Client wraps net/http.Client with HeyGen-specific behavior:
// automatic x-api-key header injection, base URL resolution, and
// User-Agent tagging. Use WithHTTPClient to inject a test transport.
type Client struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	userAgent  string
	retry      RetryConfig
}

// Option configures the Client.
type Option func(*Client)

// WithBaseURL overrides the default API base URL.
func WithBaseURL(url string) Option {
	return func(c *Client) { c.baseURL = url }
}

// WithHTTPClient injects a custom http.Client (critical for httptest).
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) { c.httpClient = hc }
}

// WithMaxRetries sets the maximum number of retries for transient failures.
// 0 disables retries. Delays remain at their defaults.
func WithMaxRetries(n int) Option {
	return func(c *Client) { c.retry.MaxRetries = n }
}

// WithUserAgent overrides the default User-Agent header.
func WithUserAgent(ua string) Option {
	return func(c *Client) { c.userAgent = ua }
}

// New creates a Client with the given API key and options.
func New(apiKey string, opts ...Option) *Client {
	c := &Client{
		httpClient: &http.Client{Timeout: DefaultTimeout},
		baseURL:    DefaultBaseURL,
		apiKey:     apiKey,
		userAgent:  DefaultUserAgent,
		retry:      DefaultRetryConfig(),
	}
	for _, opt := range opts {
		opt(c)
	}

	copied := *c.httpClient
	transport := copied.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	copied.Transport = &retryTransport{
		base:   transport,
		config: c.retry,
	}
	c.httpClient = &copied

	return c
}

// Do executes an HTTP request, injecting auth and User-Agent headers.
func (c *Client) Do(req *http.Request) (*http.Response, error) {
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("X-HeyGen-Source", "cli")
	if req.Header.Get("Content-Type") == "" && req.Body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.httpClient.Do(req)
}

// BaseURL returns the configured base URL.
func (c *Client) BaseURL() string {
	return c.baseURL
}
