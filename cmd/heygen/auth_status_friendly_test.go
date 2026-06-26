package main

import (
	"encoding/json"
	"testing"

	"github.com/heygen-com/heygen-cli/internal/auth"
)

// TestAuthStatus_SurfacesPersistedUserInfo verifies that when a user
// block is on disk (the post-login state after this change), `auth
// status` includes it under the `credential.user` key in the JSON
// envelope so consumers can read the friendly display without an extra
// /v3/users/me call.
func TestAuthStatus_SurfacesPersistedUserInfo(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("HEYGEN_CONFIG_DIR", configDir)

	// Seed: api_key + user block (post-login state).
	if err := (&auth.FileCredentialStore{}).Save("hg_test"); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := auth.SaveUserInfo(auth.UserInfo{
		Email:     "jane@example.com",
		FirstName: "Jane",
		LastName:  "Doe",
		Username:  "jdoe",
	}); err != nil {
		t.Fatalf("SaveUserInfo: %v", err)
	}

	srv := setupTestServer(t, map[string]testHandler{
		"GET /v3/users/me": {
			StatusCode: 200,
			Body:       `{"data":{"email":"jane@example.com","username":"jdoe"}}`,
		},
	})
	defer srv.Close()

	// Do NOT pass an api key on env — we want the file-resolver path so
	// credentialMetadata picks up the persisted user block (env-source
	// credentials deliberately skip the user lookup).
	res := runCommand(t, srv.URL, "", "auth", "status")
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(res.Stdout), &parsed); err != nil {
		t.Fatalf("stdout not JSON: %v\n%s", err, res.Stdout)
	}
	credMeta, ok := parsed["credential"].(map[string]any)
	if !ok {
		t.Fatalf("expected credential block in stdout, got %v", parsed)
	}
	userMeta, ok := credMeta["user"].(map[string]any)
	if !ok {
		t.Fatalf("expected credential.user block in stdout, got %v", credMeta)
	}
	if userMeta["email"] != "jane@example.com" {
		t.Errorf("credential.user.email = %v, want jane@example.com", userMeta["email"])
	}
	if userMeta["first_name"] != "Jane" {
		t.Errorf("credential.user.first_name = %v, want Jane", userMeta["first_name"])
	}
	if userMeta["last_name"] != "Doe" {
		t.Errorf("credential.user.last_name = %v, want Doe", userMeta["last_name"])
	}
	if userMeta["display_name"] != "jane@example.com" {
		t.Errorf("credential.user.display_name = %v, want jane@example.com (email)", userMeta["display_name"])
	}
}

// TestAuthStatus_NoUserBlock_FallsBack verifies the backwards-compat
// case: a credentials file without a user block (from a pre-this-change
// login) parses cleanly and `auth status` simply omits credential.user.
func TestAuthStatus_NoUserBlock_FallsBack(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("HEYGEN_CONFIG_DIR", configDir)

	if err := (&auth.FileCredentialStore{}).Save("hg_legacy"); err != nil {
		t.Fatalf("Save: %v", err)
	}

	srv := setupTestServer(t, map[string]testHandler{
		"GET /v3/users/me": {
			StatusCode: 200,
			Body:       `{"data":{"email":"u@example.com","username":"u"}}`,
		},
	})
	defer srv.Close()

	res := runCommand(t, srv.URL, "", "auth", "status")
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(res.Stdout), &parsed); err != nil {
		t.Fatalf("stdout not JSON: %v\n%s", err, res.Stdout)
	}
	credMeta, ok := parsed["credential"].(map[string]any)
	if !ok {
		t.Fatalf("expected credential block in stdout, got %v", parsed)
	}
	if _, present := credMeta["user"]; present {
		t.Errorf("credential.user should be absent for legacy file, got %v", credMeta["user"])
	}

	// Sanity: the existing /v3/users/me-derived fields are still in the
	// `data` envelope. The friendly display change is strictly additive.
	data, ok := parsed["data"].(map[string]any)
	if !ok {
		t.Fatalf("data block missing: %v", parsed)
	}
	if data["email"] != "u@example.com" {
		t.Errorf("data.email = %v, want u@example.com", data["email"])
	}
}

// TestAuthStatus_EnvSourceCredential_SkipsUserBlock — when the active
// credential came from HEYGEN_API_KEY (env), the persisted user block
// on disk could belong to a DIFFERENT api_key (the file one). We don't
// merge them in because the labels would be misleading. Env-source
// credentials therefore skip the user block.
func TestAuthStatus_EnvSourceCredential_SkipsUserBlock(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("HEYGEN_CONFIG_DIR", configDir)

	// Seed a file-side api_key + user block belonging to ANOTHER user.
	if err := (&auth.FileCredentialStore{}).Save("hg_file_key"); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := auth.SaveUserInfo(auth.UserInfo{Email: "file-user@example.com"}); err != nil {
		t.Fatalf("SaveUserInfo: %v", err)
	}

	srv := setupTestServer(t, map[string]testHandler{
		"GET /v3/users/me": {
			StatusCode: 200,
			Body:       `{"data":{"email":"env-user@example.com","username":"env"}}`,
		},
	})
	defer srv.Close()

	// Pass an api key via env — this beats the file resolver.
	res := runCommand(t, srv.URL, "hg_env_key", "auth", "status")
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(res.Stdout), &parsed); err != nil {
		t.Fatalf("stdout not JSON: %v\n%s", err, res.Stdout)
	}
	credMeta, ok := parsed["credential"].(map[string]any)
	if !ok {
		t.Fatalf("expected credential block in stdout, got %v", parsed)
	}
	if user, present := credMeta["user"]; present {
		t.Errorf("credential.user should be absent for env-source credential, got %v", user)
	}
	if credMeta["source"] != "env" {
		t.Errorf("credential.source = %v, want env", credMeta["source"])
	}
	// The data envelope reflects the /v3/users/me response under the
	// env credential, which is what the user actually wants to see.
	data, ok := parsed["data"].(map[string]any)
	if !ok || data["email"] != "env-user@example.com" {
		t.Errorf("data.email = %v, want env-user@example.com", parsed["data"])
	}
	// Cross-check: the on-disk user block was NOT touched (its account
	// has nothing to do with the env credential).
	ui, err := auth.LoadUserInfo()
	if err != nil {
		t.Fatalf("LoadUserInfo: %v", err)
	}
	if ui.Email != "file-user@example.com" {
		t.Errorf("file-side user info corrupted: %+v", ui)
	}
}
