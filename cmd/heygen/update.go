package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Masterminds/semver/v3"
	selfupdate "github.com/creativeprojects/go-selfupdate"
	clierrors "github.com/heygen-com/heygen-cli/internal/errors"
	"github.com/spf13/cobra"
)

const (
	updateRepoOwner = "heygen-com"
	updateRepoName  = "heygen-cli"
)

type updateCheckResponse struct {
	Current         string `json:"current"`
	Latest          string `json:"latest"`
	UpdateAvailable bool   `json:"update_available"`
}

type updateResponse struct {
	Previous string `json:"previous"`
	Current  string `json:"current"`
	Message  string `json:"message"`
}

type updateRelease struct {
	Version string
	raw     *selfupdate.Release
}

type releaseUpdater interface {
	DetectLatest(ctx context.Context) (updateRelease, bool, error)
	DetectVersion(ctx context.Context, version string) (updateRelease, bool, error)
	UpdateTo(ctx context.Context, rel updateRelease, cmdPath string) error
}

type selfUpdater struct {
	updater *selfupdate.Updater
	repo    selfupdate.Repository
}

func (u *selfUpdater) DetectLatest(ctx context.Context) (updateRelease, bool, error) {
	rel, found, err := u.updater.DetectLatest(ctx, u.repo)
	if err != nil || !found {
		return updateRelease{}, found, err
	}
	return updateRelease{Version: canonicalVersion(rel.Version()), raw: rel}, true, nil
}

func (u *selfUpdater) DetectVersion(ctx context.Context, version string) (updateRelease, bool, error) {
	rel, found, err := u.updater.DetectVersion(ctx, u.repo, version)
	if err != nil || !found {
		return updateRelease{}, found, err
	}
	return updateRelease{Version: canonicalVersion(rel.Version()), raw: rel}, true, nil
}

func (u *selfUpdater) UpdateTo(ctx context.Context, rel updateRelease, cmdPath string) error {
	return u.updater.UpdateTo(ctx, rel.raw, cmdPath)
}

var (
	newReleaseUpdater = func(prerelease bool) (releaseUpdater, error) {
		source, err := selfupdate.NewGitHubSource(selfupdate.GitHubConfig{
			APIToken: githubReleaseToken(),
		})
		if err != nil {
			return nil, clierrors.New(fmt.Sprintf("failed to configure update source: %v", err))
		}

		updater, err := selfupdate.NewUpdater(selfupdate.Config{
			Source:     source,
			Validator:  &selfupdate.ChecksumValidator{UniqueFilename: "checksums.txt"},
			Prerelease: prerelease,
		})
		if err != nil {
			return nil, clierrors.New(fmt.Sprintf("failed to configure updater: %v", err))
		}

		return &selfUpdater{
			updater: updater,
			repo:    selfupdate.NewRepositorySlug(updateRepoOwner, updateRepoName),
		}, nil
	}
	updateExecutablePath = selfupdate.ExecutablePath
	updateEvalSymlinks   = filepath.EvalSymlinks
	updateBuildVersion   = func(ctx *cmdContext) string { return ctx.version }
)

func newUpdateCmd(ctx *cmdContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "update",
		Short:       "Update heygen to the latest version",
		Args:        cobra.NoArgs,
		Annotations: map[string]string{"skipAuth": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			targetVersion, _ := cmd.Flags().GetString("version")
			return runUpdate(ctx, targetVersion)
		},
	}
	cmd.Flags().String("version", "", "Update to a specific version (e.g., v0.1.0)")
	cmd.AddCommand(newUpdateCheckCmd(ctx))
	return cmd
}

func newUpdateCheckCmd(ctx *cmdContext) *cobra.Command {
	return &cobra.Command{
		Use:         "check",
		Short:       "Check if a newer version is available",
		Args:        cobra.NoArgs,
		Annotations: map[string]string{"skipAuth": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUpdateCheck(ctx)
		},
	}
}

func runUpdateCheck(ctx *cmdContext) error {
	current, err := validateCurrentVersion(updateBuildVersion(ctx))
	if err != nil {
		return err
	}

	updater, err := newReleaseUpdater(false)
	if err != nil {
		return err
	}

	latest, found, err := updater.DetectLatest(context.Background())
	if err != nil {
		return clierrors.New(fmt.Sprintf("failed to check for updates: %v", err))
	}

	resp := updateCheckResponse{
		Current:         current,
		Latest:          current,
		UpdateAvailable: false,
	}
	if found {
		resp.Latest = latest.Version
		resp.UpdateAvailable = isVersionGreater(latest.Version, current)
	}

	data, err := marshalData(resp)
	if err != nil {
		return err
	}
	return ctx.formatter.Data(data, "", nil)
}

func runUpdate(ctx *cmdContext, targetVersion string) error {
	current, err := validateCurrentVersion(updateBuildVersion(ctx))
	if err != nil {
		return err
	}

	if targetVersion != "" {
		if err := validateTargetVersion(targetVersion); err != nil {
			return err
		}
	}

	method, hint, err := detectInstallMethod()
	if err != nil {
		return err
	}
	if method != "direct" {
		return &clierrors.CLIError{
			Code:     "wrong_install_method",
			Message:  fmt.Sprintf("heygen was installed via %s", method),
			Hint:     hint,
			ExitCode: clierrors.ExitGeneral,
		}
	}

	cmdPath, err := updateExecutablePath()
	if err != nil {
		return clierrors.New(fmt.Sprintf("failed to locate executable: %v", err))
	}
	cmdPath, err = updateEvalSymlinks(cmdPath)
	if err != nil {
		return clierrors.New(fmt.Sprintf("failed to resolve executable path: %v", err))
	}

	updater, err := newReleaseUpdater(false)
	if err != nil {
		return err
	}

	var rel updateRelease
	var found bool
	if targetVersion != "" {
		rel, found, err = updater.DetectVersion(context.Background(), targetVersion)
		if err != nil {
			return clierrors.New(fmt.Sprintf("failed to resolve version %s: %v", targetVersion, err))
		}
		if !found {
			return clierrors.New(fmt.Sprintf("version %s was not found for this platform", targetVersion))
		}
	} else {
		rel, found, err = updater.DetectLatest(context.Background())
		if err != nil {
			return clierrors.New(fmt.Sprintf("failed to check for updates: %v", err))
		}
		if !found || !isVersionGreater(rel.Version, current) {
			rel = updateRelease{Version: current}
		}
	}

	message := fmt.Sprintf("heygen is already at %s", current)
	if rel.Version != current {
		if err := updater.UpdateTo(context.Background(), rel, cmdPath); err != nil {
			return clierrors.New(fmt.Sprintf("failed to update heygen: %v", err))
		}
		if targetVersion != "" {
			message = fmt.Sprintf("Updated heygen to %s", rel.Version)
		} else {
			message = fmt.Sprintf("Updated heygen from %s to %s", current, rel.Version)
		}
	}

	data, err := marshalData(updateResponse{
		Previous: current,
		Current:  rel.Version,
		Message:  message,
	})
	if err != nil {
		return err
	}
	return ctx.formatter.Data(data, "", nil)
}

func validateCurrentVersion(raw string) (string, error) {
	if raw == "" || raw == "dev" || raw == "test" {
		return "", clierrors.New("current build version is not release-tagged; reinstall from a release build to use heygen update")
	}
	version := canonicalVersion(raw)
	if _, err := semver.NewVersion(strings.TrimPrefix(version, "v")); err != nil {
		return "", clierrors.New(fmt.Sprintf("current build version %q is not a valid semantic version", raw))
	}
	return version, nil
}

func validateTargetVersion(raw string) error {
	if !strings.HasPrefix(raw, "v") {
		return clierrors.NewUsage("version must include the leading v (for example: v0.1.0)")
	}
	if _, err := semver.NewVersion(strings.TrimPrefix(raw, "v")); err != nil {
		return clierrors.NewUsage(fmt.Sprintf("invalid version %q", raw))
	}
	return nil
}

func isVersionGreater(candidate, current string) bool {
	candidateSemver, err := semver.NewVersion(strings.TrimPrefix(candidate, "v"))
	if err != nil {
		return false
	}
	currentSemver, err := semver.NewVersion(strings.TrimPrefix(current, "v"))
	if err != nil {
		return false
	}
	return candidateSemver.GreaterThan(currentSemver)
}

func canonicalVersion(v string) string {
	if v == "" || strings.HasPrefix(v, "v") {
		return v
	}
	return "v" + v
}

func githubReleaseToken() string {
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		return token
	}
	if token := os.Getenv("GH_TOKEN"); token != "" {
		return token
	}
	cmd := exec.Command("gh", "auth", "token")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func detectInstallMethod() (string, string, error) {
	exe, err := updateExecutablePath()
	if err != nil {
		return "", "", clierrors.New(fmt.Sprintf("failed to locate executable: %v", err))
	}
	exe, err = updateEvalSymlinks(exe)
	if err != nil {
		return "", "", clierrors.New(fmt.Sprintf("failed to resolve executable path: %v", err))
	}

	switch {
	case strings.Contains(exe, "/homebrew/"), strings.Contains(exe, "/Cellar/"), strings.Contains(exe, "/linuxbrew/"):
		return "homebrew", "Use 'brew upgrade heygen' instead.", nil
	case strings.Contains(exe, "node_modules"):
		return "npm", "Use your package manager's update command instead.", nil
	default:
		return "direct", "", nil
	}
}
