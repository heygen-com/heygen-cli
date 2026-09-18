package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/heygen-com/heygen-cli/internal/analytics"
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
	cmd := newRootCmd(version, formatter, analytics.New("test", false))
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

func TestUpdate_UpdateAvailable(t *testing.T) {
	origFactory := newReleaseUpdater
	origExe := updateExecutablePath
	origEval := updateEvalSymlinks
	t.Cleanup(func() { newReleaseUpdater = origFactory })
	t.Cleanup(func() {
		updateExecutablePath = origExe
		updateEvalSymlinks = origEval
	})
	newReleaseUpdater = func(bool) (releaseUpdater, error) {
		mock := &mockReleaseUpdater{
			detectLatestRelease: updateRelease{Version: "v0.2.0"},
			detectLatestFound:   true,
		}
		return mock, nil
	}
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
	if got.Previous != "v0.1.0" || got.Current != "v0.2.0" || !strings.Contains(got.Message, "Updated heygen from v0.1.0 to v0.2.0") {
		t.Fatalf("unexpected response: %+v", got)
	}
}

func TestUpdate_AlreadyCurrent(t *testing.T) {
	origFactory := newReleaseUpdater
	t.Cleanup(func() { newReleaseUpdater = origFactory })
	newReleaseUpdater = func(bool) (releaseUpdater, error) {
		return &mockReleaseUpdater{
			detectLatestRelease: updateRelease{Version: "v0.1.0"},
			detectLatestFound:   true,
		}, nil
	}

	res := runUpdateRoot(t, "v0.1.0", "update")
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}

	var got updateResponse
	if err := json.Unmarshal([]byte(res.Stdout), &got); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %s", err, res.Stdout)
	}
	if got.Previous != "v0.1.0" || got.Current != "v0.1.0" || got.Message != "heygen is already at v0.1.0" {
		t.Fatalf("unexpected response: %+v", got)
	}
}

func TestUpdate_DoesNotOfferDowngradeFromNewerDevBuild(t *testing.T) {
	origFactory := newReleaseUpdater
	t.Cleanup(func() { newReleaseUpdater = origFactory })
	newReleaseUpdater = func(bool) (releaseUpdater, error) {
		return &mockReleaseUpdater{
			detectLatestRelease: updateRelease{Version: "v0.1.0"},
			detectLatestFound:   true,
		}, nil
	}

	res := runUpdateRoot(t, "v0.1.1-dev.20260406.abc1234", "update")
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}

	var got updateResponse
	if err := json.Unmarshal([]byte(res.Stdout), &got); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %s", err, res.Stdout)
	}
	if got.Previous != "v0.1.1-dev.20260406.abc1234" || got.Current != "v0.1.1-dev.20260406.abc1234" || got.Message != "heygen is already at v0.1.1-dev.20260406.abc1234" {
		t.Fatalf("unexpected response: %+v", got)
	}
}

func TestUpdate_LocalDevVersion(t *testing.T) {
	res := runUpdateRoot(t, "dev", "update")
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

// A git-describe build orders below the tag it descends from, so before this
// gate `update` treated the last release as newer and installed older code over
// a local build. The fixtures cover the shapes that must stay updatable, since
// rejecting one of those would break dev-channel updates instead.
func TestValidateCurrentVersion_AcceptsOnlyReleaseShapes(t *testing.T) {
	tests := []struct {
		name    string
		version string
		wantOK  bool
	}{
		{"stable tag", "v0.8.1", true},
		{"stable tag without v", "0.8.1", true},
		{"dev prerelease, current stamp", "v0.8.2-dev.202609180712", true},
		{"dev prerelease, older stamp with sha", "v0.1.1-dev.20260406.abc1234", true},
		{"git-describe build", "v0.8.1-6-g3ce581a-dirty", false},
		{"git-describe build, clean", "v0.8.1-6-g3ce581a", false},
		{"literal dev", "dev", false},
		{"literal test", "test", false},
		{"empty", "", false},
		{"other prerelease", "v1.0.0-rc.1", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := validateCurrentVersion(tc.version)
			if tc.wantOK && err != nil {
				t.Errorf("validateCurrentVersion(%q) = %v, want accepted", tc.version, err)
			}
			if !tc.wantOK && err == nil {
				t.Errorf("validateCurrentVersion(%q) was accepted, want rejected", tc.version)
			}
		})
	}
}

// The regression the gate exists for: a git-describe build must not be offered
// the older release it descends from.
func TestUpdate_RefusesGitDescribeBuildRatherThanDowngrading(t *testing.T) {
	origFactory := newReleaseUpdater
	t.Cleanup(func() { newReleaseUpdater = origFactory })
	newReleaseUpdater = func(bool) (releaseUpdater, error) {
		t.Error("update must not reach the release source for a non-release build")
		return &mockReleaseUpdater{}, nil
	}

	res := runUpdateRoot(t, "v0.8.1-6-g3ce581a-dirty", "update")

	if res.ExitCode != clierrors.ExitGeneral {
		t.Fatalf("ExitCode = %d, want %d\nstderr: %s", res.ExitCode, clierrors.ExitGeneral, res.Stderr)
	}
	if !strings.Contains(res.Stderr, "not release-tagged") {
		t.Errorf("stderr missing the release-tagged error: %s", res.Stderr)
	}
}

// --check reports rather than refuses, so a non-release build and a
// package-manager install are answers here instead of the errors they are in
// `update`. Each case pins one field a caller would branch on.
func TestUpdateCheck_Reports(t *testing.T) {
	origFactory := newReleaseUpdater
	origExe := updateExecutablePath
	origEval := updateEvalSymlinks
	t.Cleanup(func() {
		newReleaseUpdater = origFactory
		updateExecutablePath = origExe
		updateEvalSymlinks = origEval
	})
	updateExecutablePath = func() (string, error) { return "/usr/local/bin/heygen", nil }
	updateEvalSymlinks = func(path string) (string, error) { return path, nil }

	tests := []struct {
		name    string
		version string
		latest  string
		found   bool
		want    updateCheckResponse
	}{
		{
			name: "update available", version: "v0.1.0", latest: "v0.2.0", found: true,
			want: updateCheckResponse{Current: "v0.1.0", Latest: "v0.2.0", UpdateAvailable: true, ReleaseBuild: true, InstallMethod: "direct", Channel: "stable"},
		},
		{
			name: "already current", version: "v0.2.0", latest: "v0.2.0", found: true,
			want: updateCheckResponse{Current: "v0.2.0", Latest: "v0.2.0", UpdateAvailable: false, ReleaseBuild: true, InstallMethod: "direct", Channel: "stable"},
		},
		{
			name: "dev build tracks the dev channel", version: "v0.2.0-dev.202609180712", latest: "v0.2.0-dev.202609190000", found: true,
			want: updateCheckResponse{Current: "v0.2.0-dev.202609180712", Latest: "v0.2.0-dev.202609190000", UpdateAvailable: true, ReleaseBuild: true, InstallMethod: "direct", Channel: "dev"},
		},
		{
			name: "non-release build reports instead of erroring", version: "v0.8.1-6-g3ce581a-dirty",
			want: updateCheckResponse{Current: "v0.8.1-6-g3ce581a-dirty", UpdateAvailable: false, ReleaseBuild: false, InstallMethod: "direct"},
		},
		{
			name: "no release found for the platform", version: "v0.1.0", found: false,
			want: updateCheckResponse{Current: "v0.1.0", UpdateAvailable: false, ReleaseBuild: true, InstallMethod: "direct", Channel: "stable"},
		},
		{
			// canonicalVersion prepends "v" to anything, so canonicalizing before
			// validation reported this build as "vdev".
			name: "literal dev build keeps its raw version", version: "dev",
			want: updateCheckResponse{Current: "dev", UpdateAvailable: false, ReleaseBuild: false, InstallMethod: "direct"},
		},
		{
			// The other side of that boundary: a release build without the leading
			// v is still reported canonicalized.
			name: "release build without leading v is canonicalized", version: "0.1.0", latest: "v0.2.0", found: true,
			want: updateCheckResponse{Current: "v0.1.0", Latest: "v0.2.0", UpdateAvailable: true, ReleaseBuild: true, InstallMethod: "direct", Channel: "stable"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotPrerelease := false
			newReleaseUpdater = func(prerelease bool) (releaseUpdater, error) {
				gotPrerelease = prerelease
				return &mockReleaseUpdater{
					detectLatestRelease: updateRelease{Version: tc.latest},
					detectLatestFound:   tc.found,
				}, nil
			}

			res := runUpdateRoot(t, tc.version, "update", "--check")
			if res.ExitCode != 0 {
				t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
			}

			var got updateCheckResponse
			if err := json.Unmarshal([]byte(res.Stdout), &got); err != nil {
				t.Fatalf("stdout is not valid JSON: %v\nstdout: %s", err, res.Stdout)
			}
			got.Message = ""
			if got != tc.want {
				t.Errorf("response = %+v, want %+v", got, tc.want)
			}
			if got.ReleaseBuild && gotPrerelease != (tc.want.Channel == "dev") {
				t.Errorf("release source prerelease = %v, want %v: the reported channel must be the one actually queried",
					gotPrerelease, tc.want.Channel == "dev")
			}
		})
	}
}

// A package-manager install can still be checked: only self-installing is
// refused, and the caller needs the method to know which command to run.
func TestUpdateCheck_ReportsPackageManagerInstall(t *testing.T) {
	origFactory := newReleaseUpdater
	origExe := updateExecutablePath
	origEval := updateEvalSymlinks
	t.Cleanup(func() {
		newReleaseUpdater = origFactory
		updateExecutablePath = origExe
		updateEvalSymlinks = origEval
	})
	updateExecutablePath = func() (string, error) { return "/opt/homebrew/Cellar/heygen/0.1.0/bin/heygen", nil }
	updateEvalSymlinks = func(path string) (string, error) { return path, nil }
	newReleaseUpdater = func(bool) (releaseUpdater, error) {
		return &mockReleaseUpdater{
			detectLatestRelease: updateRelease{Version: "v0.2.0"},
			detectLatestFound:   true,
		}, nil
	}

	res := runUpdateRoot(t, "v0.1.0", "update", "--check")
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}

	var got updateCheckResponse
	if err := json.Unmarshal([]byte(res.Stdout), &got); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %s", err, res.Stdout)
	}
	if got.InstallMethod != "homebrew" {
		t.Errorf("install_method = %q, want %q", got.InstallMethod, "homebrew")
	}
	if !got.UpdateAvailable {
		t.Error("update_available = false; a newer release exists regardless of install method")
	}
}

// A failing release lookup must surface as an error, not as up-to-date.
func TestUpdateCheck_DetectLatestFailureSurfaces(t *testing.T) {
	origFactory := newReleaseUpdater
	origExe := updateExecutablePath
	origEval := updateEvalSymlinks
	t.Cleanup(func() {
		newReleaseUpdater = origFactory
		updateExecutablePath = origExe
		updateEvalSymlinks = origEval
	})
	updateExecutablePath = func() (string, error) { return "/usr/local/bin/heygen", nil }
	updateEvalSymlinks = func(path string) (string, error) { return path, nil }
	newReleaseUpdater = func(bool) (releaseUpdater, error) {
		return &mockReleaseUpdater{detectLatestErr: errors.New("network unreachable")}, nil
	}

	res := runUpdateRoot(t, "v0.1.0", "update", "--check")
	if res.ExitCode != clierrors.ExitGeneral {
		t.Fatalf("ExitCode = %d, want %d\nstderr: %s", res.ExitCode, clierrors.ExitGeneral, res.Stderr)
	}

	var envelope map[string]map[string]any
	if err := json.Unmarshal([]byte(res.Stderr), &envelope); err != nil {
		t.Fatalf("stderr is not a JSON error envelope: %v\nstderr: %s", err, res.Stderr)
	}
	if envelope["error"]["code"] != "error" {
		t.Errorf("error.code = %v, want %q", envelope["error"]["code"], "error")
	}
	if !strings.Contains(res.Stderr, "failed to check for updates") {
		t.Errorf("stderr missing the lookup failure: %s", res.Stderr)
	}
}

func TestUpdateCheck_RejectsEmptyVersionFlag(t *testing.T) {
	res := runUpdateRoot(t, "v0.1.0", "update", "--check", "--version", "")
	if res.ExitCode != clierrors.ExitUsage {
		t.Fatalf("ExitCode = %d, want %d\nstderr: %s", res.ExitCode, clierrors.ExitUsage, res.Stderr)
	}

	var envelope map[string]map[string]any
	if err := json.Unmarshal([]byte(res.Stderr), &envelope); err != nil {
		t.Fatalf("stderr is not a JSON error envelope: %v\nstderr: %s", err, res.Stderr)
	}
	if envelope["error"]["code"] != "usage_error" {
		t.Errorf("error.code = %v, want %q", envelope["error"]["code"], "usage_error")
	}
}

// SKILL.md tells callers an empty install_method means undetermined, so a
// refactor that surfaced the detection error instead would make that
// documentation wrong without failing anything else.
func TestUpdateCheck_UndeterminedInstallMethodStillReports(t *testing.T) {
	origFactory := newReleaseUpdater
	origExe := updateExecutablePath
	t.Cleanup(func() {
		newReleaseUpdater = origFactory
		updateExecutablePath = origExe
	})
	updateExecutablePath = func() (string, error) { return "", errors.New("cannot locate executable") }
	newReleaseUpdater = func(bool) (releaseUpdater, error) {
		return &mockReleaseUpdater{
			detectLatestRelease: updateRelease{Version: "v0.2.0"},
			detectLatestFound:   true,
		}, nil
	}

	res := runUpdateRoot(t, "v0.1.0", "update", "--check")
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}

	var got updateCheckResponse
	if err := json.Unmarshal([]byte(res.Stdout), &got); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %s", err, res.Stdout)
	}
	if got.InstallMethod != "" {
		t.Errorf("install_method = %q, want empty when detection fails", got.InstallMethod)
	}
	if !got.UpdateAvailable {
		t.Error("update_available = false; the version comparison must not depend on install detection")
	}
}

// The non-release path must answer without consulting the release source: it is
// the one case where no comparison is possible, and reaching out would add a
// network call to a question already answered locally.
func TestUpdateCheck_NonReleaseBuildDoesNotQueryReleaseSource(t *testing.T) {
	origFactory := newReleaseUpdater
	origExe := updateExecutablePath
	origEval := updateEvalSymlinks
	t.Cleanup(func() {
		newReleaseUpdater = origFactory
		updateExecutablePath = origExe
		updateEvalSymlinks = origEval
	})
	updateExecutablePath = func() (string, error) { return "/usr/local/bin/heygen", nil }
	updateEvalSymlinks = func(path string) (string, error) { return path, nil }
	newReleaseUpdater = func(bool) (releaseUpdater, error) {
		t.Error("--check must not reach the release source for a non-release build")
		return &mockReleaseUpdater{}, nil
	}

	res := runUpdateRoot(t, "v0.8.1-6-g3ce581a-dirty", "update", "--check")
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}
}

func TestUpdateCheck_RejectsCombinationWithVersion(t *testing.T) {
	res := runUpdateRoot(t, "v0.1.0", "update", "--check", "--version", "v0.2.0")
	if res.ExitCode != clierrors.ExitUsage {
		t.Fatalf("ExitCode = %d, want %d\nstderr: %s", res.ExitCode, clierrors.ExitUsage, res.Stderr)
	}
}
