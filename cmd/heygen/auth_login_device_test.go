package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/heygen-com/heygen-cli/internal/analytics"
	"github.com/heygen-com/heygen-cli/internal/auth"
	"github.com/heygen-com/heygen-cli/internal/config"
	"github.com/spf13/cobra"
)

func TestRunAuthLoginDeviceDispatchesExplicitDeviceFlag(t *testing.T) {
	spy := &dispatchSpy{}
	deps := spy.deps()
	deviceCalls := 0
	deps.runDevice = func(*cobra.Command, *cmdContext) error {
		deviceCalls++
		return nil
	}
	runAuthLoginTestDeps = deps
	t.Cleanup(func() { runAuthLoginTestDeps = nil })

	cmd, ctx := makeDispatchCmd(t)
	if err := runAuthLogin(cmd, ctx, authLoginFlags{deviceMode: true}); err != nil {
		t.Fatalf("runAuthLogin: %v", err)
	}
	if deviceCalls != 1 || spy.oauthCalls != 0 || spy.apiKeyCalls != 0 {
		t.Fatalf("device=%d oauth=%d apiKey=%d", deviceCalls, spy.oauthCalls, spy.apiKeyCalls)
	}
}

func TestRunDeviceLoginVerifiesIdentityBeforePersisting(t *testing.T) {
	var tokenPolls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/device":
			_, _ = w.Write([]byte(`{
				"device_code":"device-secret",
				"user_code":"ABCD-EFGH",
				"verification_uri":"` + "http://" + r.Host + `/verify",
				"expires_in":600,
				"interval":1
			}`))
		case "/token":
			tokenPolls.Add(1)
			_, _ = w.Write([]byte(`{
				"access_token":"access",
				"refresh_token":"refresh",
				"expires_in":3600,
				"scope":"openid profile",
				"token_type":"Bearer"
			}`))
		case "/v3/users/me":
			if r.Header.Get("Authorization") != "Bearer access" {
				t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
			}
			_, _ = w.Write([]byte(`{"data":{"email":"device@example.com","username":"device-user"}}`))
		default:
			t.Fatalf("unexpected request: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	configDir := t.TempDir()
	t.Setenv("HEYGEN_CONFIG_DIR", configDir)
	result := runDeviceLoginForTest(t, deviceLoginConfig{
		ClientID:               "device-client",
		DeviceAuthorizationURL: server.URL + "/device",
		TokenURL:               server.URL + "/token",
		RevokeURL:              server.URL + "/revoke",
		UsersMeBaseURL:         server.URL,
		Resource:               server.URL,
		Attended:               func() bool { return true },
		Sleep:                  func(context.Context, time.Duration) error { return nil },
	})
	if result.ExitCode != 0 {
		t.Fatalf("exit=%d stderr=%s stdout=%s", result.ExitCode, result.Stderr, result.Stdout)
	}
	if tokenPolls.Load() != 1 {
		t.Fatalf("polls=%d, want 1", tokenPolls.Load())
	}
	tokens, err := auth.LoadOAuthTokens()
	if err != nil {
		t.Fatalf("LoadOAuthTokens: %v", err)
	}
	if tokens.AccessToken != "access" || tokens.RefreshToken != "refresh" {
		t.Fatalf("tokens=%+v", tokens)
	}
	user, err := auth.LoadUserInfo()
	if err != nil {
		t.Fatalf("LoadUserInfo: %v", err)
	}
	if user.Email != "device@example.com" {
		t.Fatalf("user=%+v", user)
	}
	if strings.Contains(result.Stdout+result.Stderr, "device-secret") {
		t.Fatal("device bearer credential leaked to output")
	}
	if !strings.Contains(result.Stderr, "ABCD-EFGH") ||
		!strings.Contains(result.Stderr, "/verify") {
		t.Fatalf("stderr missing user instructions: %s", result.Stderr)
	}
}

func TestRunDeviceLoginRevokesAndDoesNotPersistWhenIdentityFails(t *testing.T) {
	var revokeCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/device":
			_, _ = w.Write([]byte(`{
				"device_code":"device-secret",
				"user_code":"ABCD-EFGH",
				"verification_uri":"` + "http://" + r.Host + `/verify",
				"expires_in":600,
				"interval":1
			}`))
		case "/token":
			_, _ = w.Write([]byte(`{
				"access_token":"access",
				"refresh_token":"refresh",
				"expires_in":3600
			}`))
		case "/v3/users/me":
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
		case "/revoke":
			revokeCalls.Add(1)
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected request: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	result := runDeviceLoginForTest(t, deviceLoginConfig{
		ClientID:               "device-client",
		DeviceAuthorizationURL: server.URL + "/device",
		TokenURL:               server.URL + "/token",
		RevokeURL:              server.URL + "/revoke",
		UsersMeBaseURL:         server.URL,
		Resource:               server.URL,
		Attended:               func() bool { return true },
		Sleep:                  func(context.Context, time.Duration) error { return nil },
	})
	if result.ExitCode == 0 {
		t.Fatalf("expected identity failure, stdout=%s", result.Stdout)
	}
	if revokeCalls.Load() != 2 {
		t.Fatalf("revoke calls=%d, want access+refresh", revokeCalls.Load())
	}
	if _, err := auth.LoadOAuthTokens(); err == nil {
		t.Fatal("tokens persisted before identity verification")
	}
}

func TestRunDeviceLoginRevokesWhenAtomicCredentialWriteFails(t *testing.T) {
	var revokeCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/device":
			_, _ = w.Write([]byte(`{
				"device_code":"device-secret",
				"user_code":"ABCD-EFGH",
				"verification_uri":"` + "http://" + r.Host + `/verify",
				"expires_in":600,
				"interval":1
			}`))
		case "/token":
			_, _ = w.Write([]byte(`{
				"access_token":"access",
				"refresh_token":"refresh",
				"expires_in":3600
			}`))
		case "/v3/users/me":
			_, _ = w.Write([]byte(`{"data":{"email":"device@example.com"}}`))
		case "/revoke":
			revokeCalls.Add(1)
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected request: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	// Make HEYGEN_CONFIG_DIR a regular file. The atomic store cannot create
	// <file>/credentials, which deterministically exercises write rollback.
	badConfigDir := t.TempDir() + "/not-a-directory"
	if err := os.WriteFile(badConfigDir, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HEYGEN_CONFIG_DIR", badConfigDir)
	result := runDeviceLoginForTest(t, deviceLoginConfig{
		ClientID:               "device-client",
		DeviceAuthorizationURL: server.URL + "/device",
		TokenURL:               server.URL + "/token",
		RevokeURL:              server.URL + "/revoke",
		UsersMeBaseURL:         server.URL,
		Resource:               server.URL,
		Attended:               func() bool { return true },
		Sleep:                  func(context.Context, time.Duration) error { return nil },
	})
	if result.ExitCode == 0 {
		t.Fatal("expected credential write failure")
	}
	if revokeCalls.Load() != 2 {
		t.Fatalf("revoke calls=%d, want access+refresh", revokeCalls.Load())
	}
	if strings.Contains(result.Stdout+result.Stderr, "device-secret") {
		t.Fatal("device bearer credential leaked to output")
	}
}

func TestRunDeviceLoginRefusesUnattendedEnvironment(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	result := runDeviceLoginForTest(t, deviceLoginConfig{
		Attended: func() bool { return false },
	})
	if result.ExitCode == 0 {
		t.Fatal("expected unattended device login to fail")
	}
	if !strings.Contains(result.Stderr, "attended") {
		t.Fatalf("stderr=%q", result.Stderr)
	}
}

func runDeviceLoginForTest(t *testing.T, cfg deviceLoginConfig) cmdResult {
	t.Helper()
	var stdout, stderr strings.Builder
	formatter := formatterForArgs([]string{"auth", "login", "--device"}, &stdout, &stderr)
	if os.Getenv("HEYGEN_CONFIG_DIR") == "" {
		t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	}
	t.Setenv("HEYGEN_NO_ANALYTICS", "1")
	ctx := &cmdContext{
		formatter: formatter,
		version:   "test",
		configProvider: &config.LayeredProvider{
			Env:  &config.EnvProvider{},
			File: &config.FileProvider{},
		},
	}
	_ = analytics.New("test", false)
	cmd := newAuthLoginCmd(ctx)
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.RunE = func(c *cobra.Command, _ []string) error {
		return runDeviceLogin(c, ctx, cfg)
	}
	exitCode := 0
	if err := cmd.Execute(); err != nil {
		exitCode = 1
		_, _ = stderr.WriteString(err.Error())
	}
	return cmdResult{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: exitCode}
}
