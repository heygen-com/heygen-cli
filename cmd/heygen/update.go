package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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

type updateResponse struct {
	Previous string `json:"previous"`
	Current  string `json:"current"`
	Message  string `json:"message"`
}

type updateCheckResponse struct {
	Current         string `json:"current"`
	Latest          string `json:"latest"`
	UpdateAvailable bool   `json:"update_available"`
	ReleaseBuild    bool   `json:"release_build"`
	InstallMethod   string `json:"install_method"`
	Channel         string `json:"channel"`
	Message         string `json:"message"`
}

// releaseTaggedVersion matches a stable tag or a dev prerelease. The dev suffix
// stays loose on purpose: dev-release.yml stamps a timestamp while the fixtures
// below carry a timestamp plus a sha, so the marker is what identifies the
// channel, not the stamp's shape.
//
// It is a shape check rather than a semver parse because git-describe is what it
// must reject, and "v0.8.1-6-gabc1234-dirty" parses fine as semver while
// ordering BELOW v0.8.1. Accepting it makes the newest release compare as newer,
// so an "update" installs older code than the build already running.
var releaseTaggedVersion = regexp.MustCompile(`^v\d+\.\d+\.\d+(-dev\.[0-9A-Za-z.]+)?$`)

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
		Short:       "Check for and install newer versions of heygen",
		Args:        cobra.NoArgs,
		Annotations: map[string]string{"skipAuth": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			targetVersion, _ := cmd.Flags().GetString("version")
			check, _ := cmd.Flags().GetBool("check")
			if check {
				if cmd.Flags().Changed("version") {
					return clierrors.NewUsage("--check reports what an update would do; it cannot be combined with --version")
				}
				return runUpdateCheck(ctx)
			}
			return runUpdate(ctx, targetVersion)
		},
	}
	cmd.Flags().String("version", "", "Update to a specific version (e.g., v0.1.0)")
	cmd.Flags().Bool("check", false, "Report whether an update is available without installing it")
	return cmd
}

// runUpdateCheck answers "what would heygen update do", without doing it.
//
// It reports rather than refuses, which is the whole difference from runUpdate:
// a non-release build and a package-manager install are both terminal errors
// there, but here they are the answer, so a caller can branch on the fields
// instead of parsing an error.
func runUpdateCheck(ctx *cmdContext) error {
	raw := updateBuildVersion(ctx)
	resp := updateCheckResponse{Current: raw}

	// Best-effort: an install method we cannot determine still permits the
	// version comparison, which is the part the caller asked for.
	if method, _, err := detectInstallMethod(); err == nil {
		resp.InstallMethod = method
	}

	current, err := validateCurrentVersion(raw)
	if err != nil {
		resp.Message = "not a release build, so no update can be offered"
		return emitUpdateCheck(ctx, resp)
	}
	resp.Current = current
	resp.ReleaseBuild = true
	resp.Channel = updateChannel(current)

	updater, err := newReleaseUpdater(resp.Channel == "dev")
	if err != nil {
		return err
	}
	rel, found, err := updater.DetectLatest(context.Background())
	if err != nil {
		return clierrors.New(fmt.Sprintf("failed to check for updates: %v", err))
	}
	if !found {
		resp.Message = fmt.Sprintf("no %s release found for this platform", resp.Channel)
		return emitUpdateCheck(ctx, resp)
	}

	resp.Latest = rel.Version
	resp.UpdateAvailable = isVersionGreater(rel.Version, current)
	if resp.UpdateAvailable {
		resp.Message = fmt.Sprintf("heygen %s is available; you have %s", rel.Version, current)
	} else {
		resp.Message = fmt.Sprintf("heygen is up to date at %s", current)
	}
	return emitUpdateCheck(ctx, resp)
}

func emitUpdateCheck(ctx *cmdContext, resp updateCheckResponse) error {
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

	updater, err := newReleaseUpdater(updateChannel(current) == "dev")
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
	version := canonicalVersion(raw)
	if !releaseTaggedVersion.MatchString(version) {
		return "", clierrors.New("current build version is not release-tagged; reinstall from a release build to use heygen update")
	}
	if _, err := semver.NewVersion(strings.TrimPrefix(version, "v")); err != nil {
		return "", clierrors.New(fmt.Sprintf("current build version %q is not a valid semantic version", raw))
	}
	return version, nil
}

// updateChannel selects the release track, which becomes the updater's
// prerelease flag: a dev build sees dev prereleases, a stable build never does.
func updateChannel(version string) string {
	if strings.Contains(version, "-dev.") {
		return "dev"
	}
	return "stable"
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
