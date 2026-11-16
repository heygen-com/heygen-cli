package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/creativeprojects/go-selfupdate/update"
)

const (
	updateHTTPTimeout       = 2 * time.Minute
	updateChecksumsMaxBytes = 16 << 10
	updateArchiveMaxBytes   = 64 << 20
	updateBinaryMaxBytes    = 256 << 20
)

var (
	// updateCDNBaseURL is where install.sh installs from, so neither update nor
	// the notice can name or install a release the installer would not serve.
	updateCDNBaseURL = "https://static.heygen.ai/cli"

	// Redirects are refused so every byte comes from updateCDNBaseURL itself.
	updateHTTPClient = &http.Client{
		Timeout: updateHTTPTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// stableReleaseTag is the tag shape the stable release workflow enforces.
	// Every CDN URL is built only from a value that matches it.
	stableReleaseTag = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

	errReleaseNotFound = errors.New("release not found")
)

// cdnUpdater installs stable releases from the CDN, verifying each archive
// against the checksums.txt published beside it.
type cdnUpdater struct {
	goos, goarch string
}

func (u *cdnUpdater) DetectLatest(ctx context.Context) (updateRelease, bool, error) {
	tag, err := fetchStablePointer(ctx)
	if err != nil {
		return updateRelease{}, false, err
	}
	return updateRelease{Version: tag}, true, nil
}

func (u *cdnUpdater) DetectVersion(ctx context.Context, version string) (updateRelease, bool, error) {
	if !stableReleaseTag.MatchString(version) {
		return updateRelease{}, false, nil
	}
	if _, err := u.expectedChecksum(ctx, version); err != nil {
		if errors.Is(err, errReleaseNotFound) {
			return updateRelease{}, false, nil
		}
		return updateRelease{}, false, err
	}
	return updateRelease{Version: version}, true, nil
}

func (u *cdnUpdater) UpdateTo(ctx context.Context, rel updateRelease, cmdPath string) error {
	tag := rel.Version
	if !stableReleaseTag.MatchString(tag) {
		return fmt.Errorf("%q is not a stable release tag", tag)
	}
	want, err := u.expectedChecksum(ctx, tag)
	if err != nil {
		return err
	}
	asset := u.assetName(tag)
	archive, err := cdnGet(ctx, releaseFileURL(tag, asset), updateArchiveMaxBytes)
	if err != nil {
		return err
	}
	got := sha256.Sum256(archive)
	if hex.EncodeToString(got[:]) != want {
		return fmt.Errorf("checksum mismatch for %s", asset)
	}
	binary, err := extractBinary(archive, asset, u.binaryName())
	if err != nil {
		return err
	}
	return update.Apply(bytes.NewReader(binary), update.Options{TargetPath: cmdPath})
}

// expectedChecksum returns the sha256 that tag's checksums.txt lists for this
// platform's archive. A missing release or a missing entry wraps
// errReleaseNotFound.
func (u *cdnUpdater) expectedChecksum(ctx context.Context, tag string) (string, error) {
	data, err := cdnGet(ctx, releaseFileURL(tag, "checksums.txt"), updateChecksumsMaxBytes)
	if err != nil {
		return "", err
	}
	asset := u.assetName(tag)
	sum := ""
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[1] != asset {
			continue
		}
		if sum != "" {
			return "", fmt.Errorf("checksums.txt for %s lists %s more than once", tag, asset)
		}
		sum = strings.ToLower(fields[0])
	}
	if sum == "" {
		return "", fmt.Errorf("%w: checksums.txt for %s has no entry for %s", errReleaseNotFound, tag, asset)
	}
	if decoded, err := hex.DecodeString(sum); err != nil || len(decoded) != sha256.Size {
		return "", fmt.Errorf("checksums.txt for %s has a malformed entry for %s", tag, asset)
	}
	return sum, nil
}

// assetName follows the archives name_template in .goreleaser.yaml.
func (u *cdnUpdater) assetName(tag string) string {
	ext := ".tar.gz"
	if u.goos == "windows" {
		ext = ".zip"
	}
	return fmt.Sprintf("heygen_%s_%s_%s%s", tag, u.goos, u.goarch, ext)
}

func (u *cdnUpdater) binaryName() string {
	if u.goos == "windows" {
		return "heygen.exe"
	}
	return "heygen"
}

func releaseFileURL(tag, name string) string {
	return updateCDNBaseURL + "/releases/" + tag + "/" + name
}

// cdnGet reads at most maxBytes. The CDN answers 403, not 404, for an object
// that does not exist, so both wrap errReleaseNotFound.
func cdnGet(ctx context.Context, url string, maxBytes int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := updateHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound:
		return nil, fmt.Errorf("%w: %s returned HTTP %d", errReleaseNotFound, url, resp.StatusCode)
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("%s returned HTTP %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxBytes {
		return nil, fmt.Errorf("%s exceeds %d bytes", url, maxBytes)
	}
	return body, nil
}

// extractBinary returns the regular file named exactly binary at the archive
// root. Nothing is written using an entry's own name.
func extractBinary(archive []byte, asset, binary string) ([]byte, error) {
	var r io.Reader
	if strings.HasSuffix(asset, ".zip") {
		zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", asset, err)
		}
		for _, f := range zr.File {
			if f.Name == binary && f.Mode().IsRegular() {
				rc, err := f.Open()
				if err != nil {
					return nil, fmt.Errorf("reading %s: %w", asset, err)
				}
				defer rc.Close()
				r = rc
				break
			}
		}
	} else {
		gz, err := gzip.NewReader(bytes.NewReader(archive))
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", asset, err)
		}
		tr := tar.NewReader(gz)
		for {
			h, err := tr.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return nil, fmt.Errorf("reading %s: %w", asset, err)
			}
			if h.Name == binary && h.Typeflag == tar.TypeReg {
				r = tr
				break
			}
		}
	}
	if r == nil {
		return nil, fmt.Errorf("%s does not contain %s", asset, binary)
	}
	data, err := io.ReadAll(io.LimitReader(r, updateBinaryMaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", asset, err)
	}
	if len(data) > updateBinaryMaxBytes {
		return nil, fmt.Errorf("%s in %s exceeds %d bytes", binary, asset, updateBinaryMaxBytes)
	}
	return data, nil
}
