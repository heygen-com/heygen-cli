package oauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRequestDeviceAuthorization(t *testing.T) {
	var got url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/device" {
			t.Fatalf("path = %q, want /device", r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		got = r.PostForm
		_ = json.NewEncoder(w).Encode(map[string]any{
			"device_code":      "device-secret",
			"user_code":        "ABCD-EFGH",
			"verification_uri": "https://app.heygen.com/oauth/device",
			"expires_in":       600,
			"interval":         5,
		})
	}))
	defer server.Close()

	client := NewClient(
		WithClientID("cli-id"),
		WithDeviceAuthorizationURL(server.URL+"/device"),
		WithHTTPClient(server.Client()),
	)
	gotResponse, err := client.RequestDeviceAuthorization(
		context.Background(),
		"openid profile",
		"https://api.heygen.com",
	)
	if err != nil {
		t.Fatalf("RequestDeviceAuthorization: %v", err)
	}
	if got.Get("client_id") != "cli-id" ||
		got.Get("scope") != "openid profile" ||
		got.Get("resource") != "https://api.heygen.com" {
		t.Fatalf("form = %#v", got)
	}
	if gotResponse.DeviceCode != "device-secret" ||
		gotResponse.UserCode != "ABCD-EFGH" ||
		gotResponse.Interval != 5 {
		t.Fatalf("response = %+v", gotResponse)
	}
}

func TestRequestDeviceAuthorizationClampsIntervalToMinimum(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"device_code":      "device-secret",
			"user_code":        "ABCD-EFGH",
			"verification_uri": "https://app.heygen.com/oauth/device",
			"expires_in":       600,
			"interval":         1,
		})
	}))
	defer server.Close()

	client := NewClient(
		WithDeviceAuthorizationURL(server.URL),
		WithHTTPClient(server.Client()),
	)
	got, err := client.RequestDeviceAuthorization(context.Background(), "", "")
	if err != nil {
		t.Fatalf("RequestDeviceAuthorization: %v", err)
	}
	if got.Interval != 5 {
		t.Fatalf("interval = %d, want 5", got.Interval)
	}
}

func TestRequestDeviceAuthorizationRejectsUnsafeVerificationURI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"device_code":      "device-secret",
			"user_code":        "ABCD-EFGH",
			"verification_uri": "http://attacker.example/oauth/device",
			"expires_in":       600,
			"interval":         5,
		})
	}))
	defer server.Close()

	client := NewClient(
		WithDeviceAuthorizationURL(server.URL),
		WithHTTPClient(server.Client()),
	)
	if _, err := client.RequestDeviceAuthorization(context.Background(), "", ""); err == nil {
		t.Fatal("expected unsafe verification URI to be rejected")
	}
}

func TestRequestDeviceAuthorizationRejectsTerminalControlInUserCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"device_code":      "device-secret",
			"user_code":        "ABCD-\x1b[31mEFGH",
			"verification_uri": "https://app.heygen.com/oauth/device",
			"expires_in":       600,
			"interval":         5,
		})
	}))
	defer server.Close()

	client := NewClient(
		WithDeviceAuthorizationURL(server.URL),
		WithHTTPClient(server.Client()),
	)
	if _, err := client.RequestDeviceAuthorization(context.Background(), "", ""); err == nil {
		t.Fatal("expected terminal control characters in user_code to be rejected")
	}
}

func TestPollDeviceTokenPendingSlowDownThenSuccess(t *testing.T) {
	var mu sync.Mutex
	polls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.PostForm.Get("device_code") != "device-secret" {
			t.Fatalf("device_code = %q", r.PostForm.Get("device_code"))
		}
		mu.Lock()
		defer mu.Unlock()
		polls++
		switch polls {
		case 1:
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"authorization_pending"}`))
		case 2:
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"slow_down"}`))
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "access",
				"refresh_token": "refresh",
				"expires_in":    3600,
				"scope":         "openid profile",
				"token_type":    "Bearer",
			})
		}
	}))
	defer server.Close()

	now := time.Unix(1_000, 0)
	var waits []time.Duration
	client := NewClient(
		WithTokenURL(server.URL),
		WithHTTPClient(server.Client()),
		WithNow(func() time.Time { return now }),
		WithSleep(func(_ context.Context, d time.Duration) error {
			waits = append(waits, d)
			now = now.Add(d)
			return nil
		}),
	)
	tok, err := client.PollDeviceToken(context.Background(), "device-secret", 5, 600)
	if err != nil {
		t.Fatalf("PollDeviceToken: %v", err)
	}
	if tok.AccessToken != "access" || tok.RefreshToken != "refresh" {
		t.Fatalf("token = %+v", tok)
	}
	want := []time.Duration{5 * time.Second, 5 * time.Second, 10 * time.Second}
	if len(waits) != len(want) {
		t.Fatalf("waits = %v, want %v", waits, want)
	}
	for i := range want {
		if waits[i] != want[i] {
			t.Fatalf("waits = %v, want %v", waits, want)
		}
	}
}

func TestPollDeviceTokenClampsIntervalToMinimum(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "access",
			"expires_in":   3600,
			"token_type":   "Bearer",
		})
	}))
	defer server.Close()

	now := time.Unix(1_000, 0)
	var waits []time.Duration
	client := NewClient(
		WithTokenURL(server.URL),
		WithHTTPClient(server.Client()),
		WithNow(func() time.Time { return now }),
		WithSleep(func(_ context.Context, d time.Duration) error {
			waits = append(waits, d)
			now = now.Add(d)
			return nil
		}),
	)
	if _, err := client.PollDeviceToken(context.Background(), "device-secret", 1, 600); err != nil {
		t.Fatalf("PollDeviceToken: %v", err)
	}
	if len(waits) != 1 || waits[0] != 5*time.Second {
		t.Fatalf("waits = %v, want [5s]", waits)
	}
}

func TestPollDeviceTokenTreatsBare429AsSlowDown(t *testing.T) {
	var polls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		polls++
		if polls == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("rate limited"))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "access",
			"expires_in":   3600,
			"token_type":   "Bearer",
		})
	}))
	defer server.Close()

	now := time.Unix(1_000, 0)
	var waits []time.Duration
	client := NewClient(
		WithTokenURL(server.URL),
		WithHTTPClient(server.Client()),
		WithNow(func() time.Time { return now }),
		WithSleep(func(_ context.Context, d time.Duration) error {
			waits = append(waits, d)
			now = now.Add(d)
			return nil
		}),
	)
	if _, err := client.PollDeviceToken(context.Background(), "device-secret", 5, 600); err != nil {
		t.Fatalf("PollDeviceToken: %v", err)
	}
	want := []time.Duration{5 * time.Second, 10 * time.Second}
	if len(waits) != len(want) {
		t.Fatalf("waits = %v, want %v", waits, want)
	}
	for i := range want {
		if waits[i] != want[i] {
			t.Fatalf("waits = %v, want %v", waits, want)
		}
	}
}

func TestPollDeviceTokenTerminalErrorsDoNotEchoResponseBody(t *testing.T) {
	for _, code := range []string{"access_denied", "expired_token", "invalid_grant"} {
		t.Run(code, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"` + code + `","error_description":"device-secret"}`))
			}))
			defer server.Close()

			client := NewClient(
				WithTokenURL(server.URL),
				WithHTTPClient(server.Client()),
				WithSleep(func(context.Context, time.Duration) error { return nil }),
			)
			_, err := client.PollDeviceToken(context.Background(), "device-secret", 1, 30)
			if err == nil {
				t.Fatal("expected terminal error")
			}
			if strings.Contains(err.Error(), "device-secret") {
				t.Fatalf("error leaked device code: %q", err)
			}
			if !strings.Contains(err.Error(), code) {
				t.Fatalf("error = %q, want %q", err, code)
			}
		})
	}
}
