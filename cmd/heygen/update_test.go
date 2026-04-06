package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	clierrors "github.com/heygen-com/heygen-cli/internal/errors"
)

type mockReleaseUpdater struct {
	detectLatestRelease  updateRelease
	detectLatestFound    bool
	detectLatestErr      error
	detectVersionRelease updateRelease
	detectVersionFound   bool
	detectVersionErr     error
	updateErr            error
	updatedTo            string
}

func (m *mockReleaseUpdater) DetectLatest(context.Context) (updateRelease, bool, error) {
	return m.detectLatestRelease, m.detectLatestFound, m.detectLatestErr
}

func (m *mockReleaseUpdater) DetectVersion(context.Context, string) (updateRelease, bool, error) {
	return m.detectVersionRelease, m.detectVersionFound, m.detectVersionErr
}

func (m *mockReleaseUpdater) UpdateTo(_ context.Context, rel updateRelease, _ string) error {
	m.updatedTo = rel.Version
	return m.updateErr
}

func runUpdateRoot(t *testing.T, version string, args ...string) cmdResult {
	t.Helper()

	var stdout, stderr bytes.Buffer
	formatter := formatterForArgs(args, &stdout, &stderr)
	t.Setenv("HEYGEN_API_KEY", "")
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	cmd := newRootCmd(version, formatter)
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)

	err := cmd.Execute()
	exitCode := 0
	if err != nil {
		var cliErr *clierrors.CLIError
		if errors.As(err, &cliErr) {
			formatter.Error(cliErr)
			exitCode = cliErr.ExitCode
		} else {
			wrapped := classifyError(err)
			formatter.Error(wrapped)
			exitCode = wrapped.ExitCode
		}
	}

	return cmdResult{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: exitCode}
}

func TestUpdateCheck_NewVersionAvailable(t *testing.T) {
	origFactory := newReleaseUpdater
	t.Cleanup(func() { newReleaseUpdater = origFactory })
	newReleaseUpdater = func(bool) (releaseUpdater, error) {
		return &mockReleaseUpdater{
			detectLatestRelease: updateRelease{Version: "v0.2.0"},
			detectLatestFound:   true,
		}, nil
	}

	res := runUpdateRoot(t, "v0.1.0", "update", "check")
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}

	var got updateCheckResponse
	if err := json.Unmarshal([]byte(res.Stdout), &got); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %s", err, res.Stdout)
	}
	if got.Current != "v0.1.0" || got.Latest != "v0.2.0" || !got.UpdateAvailable {
		t.Fatalf("unexpected response: %+v", got)
	}
}

func TestUpdateCheck_AlreadyCurrent(t *testing.T) {
	origFactory := newReleaseUpdater
	t.Cleanup(func() { newReleaseUpdater = origFactory })
	newReleaseUpdater = func(bool) (releaseUpdater, error) {
		return &mockReleaseUpdater{
			detectLatestRelease: updateRelease{Version: "v0.1.0"},
			detectLatestFound:   true,
		}, nil
	}

	res := runUpdateRoot(t, "v0.1.0", "update", "check")
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}

	var got updateCheckResponse
	if err := json.Unmarshal([]byte(res.Stdout), &got); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %s", err, res.Stdout)
	}
	if got.UpdateAvailable {
		t.Fatalf("UpdateAvailable = true, want false")
	}
}

func TestUpdateCheck_DoesNotOfferDowngradeFromNewerDevBuild(t *testing.T) {
	origFactory := newReleaseUpdater
	t.Cleanup(func() { newReleaseUpdater = origFactory })
	newReleaseUpdater = func(bool) (releaseUpdater, error) {
		return &mockReleaseUpdater{
			detectLatestRelease: updateRelease{Version: "v0.1.0"},
			detectLatestFound:   true,
		}, nil
	}

	res := runUpdateRoot(t, "v0.1.1-dev.20260406.abc1234", "update", "check")
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}

	var got updateCheckResponse
	if err := json.Unmarshal([]byte(res.Stdout), &got); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %s", err, res.Stdout)
	}
	if got.Current != "v0.1.1-dev.20260406.abc1234" || got.Latest != "v0.1.0" || got.UpdateAvailable {
		t.Fatalf("unexpected response: %+v", got)
	}
}

func TestUpdateCheck_LocalDevVersion(t *testing.T) {
	res := runUpdateRoot(t, "dev", "update", "check")
	if res.ExitCode != clierrors.ExitGeneral {
		t.Fatalf("ExitCode = %d, want %d\nstderr: %s", res.ExitCode, clierrors.ExitGeneral, res.Stderr)
	}
	if !strings.Contains(res.Stderr, "not release-tagged") {
		t.Fatalf("stderr missing local build error: %s", res.Stderr)
	}
}

func TestUpdate_SkipsAuth(t *testing.T) {
	origFactory := newReleaseUpdater
	origExe := updateExecutablePath
	origEval := updateEvalSymlinks
	t.Cleanup(func() {
		newReleaseUpdater = origFactory
		updateExecutablePath = origExe
		updateEvalSymlinks = origEval
	})

	mock := &mockReleaseUpdater{
		detectLatestRelease: updateRelease{Version: "v0.1.0"},
		detectLatestFound:   true,
	}
	newReleaseUpdater = func(bool) (releaseUpdater, error) { return mock, nil }
	updateExecutablePath = func() (string, error) { return "/usr/local/bin/heygen", nil }
	updateEvalSymlinks = func(path string) (string, error) { return path, nil }

	res := runUpdateRoot(t, "v0.1.0", "update")
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}
}

func TestUpdate_PackageManagerWarning(t *testing.T) {
	origExe := updateExecutablePath
	origEval := updateEvalSymlinks
	t.Cleanup(func() {
		updateExecutablePath = origExe
		updateEvalSymlinks = origEval
	})
	updateExecutablePath = func() (string, error) { return "/opt/homebrew/bin/heygen", nil }
	updateEvalSymlinks = func(path string) (string, error) { return path, nil }

	res := runUpdateRoot(t, "v0.1.0", "update")
	if res.ExitCode != clierrors.ExitGeneral {
		t.Fatalf("ExitCode = %d, want %d\nstderr: %s", res.ExitCode, clierrors.ExitGeneral, res.Stderr)
	}
	if !strings.Contains(res.Stderr, "wrong_install_method") || !strings.Contains(res.Stderr, "brew upgrade heygen") {
		t.Fatalf("stderr missing install method guidance: %s", res.Stderr)
	}
}

func TestUpdate_JSONOutput(t *testing.T) {
	origFactory := newReleaseUpdater
	origExe := updateExecutablePath
	origEval := updateEvalSymlinks
	t.Cleanup(func() {
		newReleaseUpdater = origFactory
		updateExecutablePath = origExe
		updateEvalSymlinks = origEval
	})

	mock := &mockReleaseUpdater{
		detectLatestRelease: updateRelease{Version: "v0.2.0"},
		detectLatestFound:   true,
	}
	newReleaseUpdater = func(bool) (releaseUpdater, error) { return mock, nil }
	updateExecutablePath = func() (string, error) { return "/usr/local/bin/heygen", nil }
	updateEvalSymlinks = func(path string) (string, error) { return path, nil }

	res := runUpdateRoot(t, "v0.1.0", "update")
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}

	var got updateResponse
	if err := json.Unmarshal([]byte(res.Stdout), &got); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %s", err, res.Stdout)
	}
	if got.Previous != "v0.1.0" || got.Current != "v0.2.0" || mock.updatedTo != "v0.2.0" {
		t.Fatalf("unexpected response: %+v updatedTo=%s", got, mock.updatedTo)
	}
}

func TestUpdate_DoesNotDowngradeFromNewerDevBuildByDefault(t *testing.T) {
	origFactory := newReleaseUpdater
	origExe := updateExecutablePath
	origEval := updateEvalSymlinks
	t.Cleanup(func() {
		newReleaseUpdater = origFactory
		updateExecutablePath = origExe
		updateEvalSymlinks = origEval
	})

	mock := &mockReleaseUpdater{
		detectLatestRelease: updateRelease{Version: "v0.1.0"},
		detectLatestFound:   true,
	}
	newReleaseUpdater = func(bool) (releaseUpdater, error) { return mock, nil }
	updateExecutablePath = func() (string, error) { return "/usr/local/bin/heygen", nil }
	updateEvalSymlinks = func(path string) (string, error) { return path, nil }

	res := runUpdateRoot(t, "v0.1.1-dev.20260406.abc1234", "update")
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}

	var got updateResponse
	if err := json.Unmarshal([]byte(res.Stdout), &got); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %s", err, res.Stdout)
	}
	if got.Previous != "v0.1.1-dev.20260406.abc1234" || got.Current != "v0.1.1-dev.20260406.abc1234" {
		t.Fatalf("unexpected response: %+v", got)
	}
	if mock.updatedTo != "" {
		t.Fatalf("unexpected downgrade attempt to %s", mock.updatedTo)
	}
}

func TestUpdate_ExplicitVersionAllowsDowngrade(t *testing.T) {
	origFactory := newReleaseUpdater
	origExe := updateExecutablePath
	origEval := updateEvalSymlinks
	t.Cleanup(func() {
		newReleaseUpdater = origFactory
		updateExecutablePath = origExe
		updateEvalSymlinks = origEval
	})

	mock := &mockReleaseUpdater{
		detectVersionRelease: updateRelease{Version: "v0.1.0"},
		detectVersionFound:   true,
	}
	newReleaseUpdater = func(bool) (releaseUpdater, error) { return mock, nil }
	updateExecutablePath = func() (string, error) { return "/usr/local/bin/heygen", nil }
	updateEvalSymlinks = func(path string) (string, error) { return path, nil }

	res := runUpdateRoot(t, "v0.1.1-dev.20260406.abc1234", "update", "--version", "v0.1.0")
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}

	var got updateResponse
	if err := json.Unmarshal([]byte(res.Stdout), &got); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %s", err, res.Stdout)
	}
	if got.Previous != "v0.1.1-dev.20260406.abc1234" || got.Current != "v0.1.0" || mock.updatedTo != "v0.1.0" {
		t.Fatalf("unexpected response: %+v updatedTo=%s", got, mock.updatedTo)
	}
}

func TestUpdate_RejectsBareVersion(t *testing.T) {
	res := runUpdateRoot(t, "v0.2.0", "update", "--version", "0.1.0")
	if res.ExitCode != clierrors.ExitUsage {
		t.Fatalf("ExitCode = %d, want %d\nstderr: %s", res.ExitCode, clierrors.ExitUsage, res.Stderr)
	}
}
