package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	clierrors "github.com/heygen-com/heygen-cli/internal/errors"
)

const installedBinary = "installed-binary"

type cdnObject struct {
	status   int
	body     []byte
	location string
}

type fakeCDN struct {
	mu       sync.Mutex
	objects  map[string]cdnObject
	requests []string
}

func (f *fakeCDN) requested() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.requests...)
}

// setupCDNUpdate serves objects under updateCDNBaseURL (any other path is 403,
// as on the real CDN) and points the update at a stand-in binary it returns.
func setupCDNUpdate(t *testing.T, objects map[string]cdnObject) (*fakeCDN, string) {
	t.Helper()
	cdn := &fakeCDN{objects: objects}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/cli/")
		cdn.mu.Lock()
		cdn.requests = append(cdn.requests, path)
		cdn.mu.Unlock()
		obj, ok := objects[path]
		if !ok {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if obj.location != "" {
			w.Header().Set("Location", obj.location)
		}
		if obj.status == 0 {
			obj.status = http.StatusOK
		}
		w.WriteHeader(obj.status)
		_, _ = w.Write(obj.body)
	}))
	t.Cleanup(srv.Close)

	binPath := filepath.Join(t.TempDir(), "heygen")
	if err := os.WriteFile(binPath, []byte(installedBinary), 0o755); err != nil {
		t.Fatal(err)
	}
	origBase, origExe, origEval := updateCDNBaseURL, updateExecutablePath, updateEvalSymlinks
	t.Cleanup(func() {
		updateCDNBaseURL, updateExecutablePath, updateEvalSymlinks = origBase, origExe, origEval
	})
	updateCDNBaseURL = srv.URL + "/cli"
	updateExecutablePath = func() (string, error) { return binPath, nil }
	updateEvalSymlinks = func(path string) (string, error) { return path, nil }
	return cdn, binPath
}

// platformAsset is the archive name install.sh downloads for this platform.
func platformAsset(tag string) string {
	ext := ".tar.gz"
	if runtime.GOOS == "windows" {
		ext = ".zip"
	}
	return fmt.Sprintf("heygen_%s_%s_%s%s", tag, runtime.GOOS, runtime.GOARCH, ext)
}

func platformBinary() string {
	if runtime.GOOS == "windows" {
		return "heygen.exe"
	}
	return "heygen"
}

// buildArchive packs one file the way GoReleaser does for this platform.
func buildArchive(t *testing.T, entry string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	if runtime.GOOS == "windows" {
		zw := zip.NewWriter(&buf)
		w, err := zw.Create(entry)
		if err == nil {
			_, err = w.Write(content)
		}
		if err == nil {
			err = zw.Close()
		}
		if err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	err := tw.WriteHeader(&tar.Header{Name: entry, Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg})
	if err == nil {
		_, err = tw.Write(content)
	}
	if err == nil {
		err = tw.Close()
	}
	if err == nil {
		err = gz.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// releaseObjects publishes tag with a correct checksums.txt for archive.
func releaseObjects(tag string, archive []byte) map[string]cdnObject {
	asset := platformAsset(tag)
	checksums := fmt.Sprintf("%s  heygen_%s_plan9_amd64.tar.gz\n%s  %s\n", strings.Repeat("0", 64), tag, sha256Hex(archive), asset)
	return map[string]cdnObject{
		"releases/" + tag + "/" + asset:      {body: archive},
		"releases/" + tag + "/checksums.txt": {body: []byte(checksums)},
	}
}

func readBinary(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestUpdate_StableInstallsVerifiedArchiveFromCDN(t *testing.T) {
	objects := releaseObjects("v0.2.0", buildArchive(t, platformBinary(), []byte("v0.2.0 binary")))
	objects["stable"] = cdnObject{body: []byte("v0.2.0\n")}
	_, binPath := setupCDNUpdate(t, objects)

	res := runUpdateRoot(t, "v0.1.0", "update")
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}
	var got updateResponse
	if err := json.Unmarshal([]byte(res.Stdout), &got); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %s", err, res.Stdout)
	}
	if got.Current != "v0.2.0" {
		t.Errorf("current = %q, want the version the stable pointer names", got.Current)
	}
	if bin := readBinary(t, binPath); bin != "v0.2.0 binary" {
		t.Errorf("installed binary = %q, want the archive's binary", bin)
	}
}

// Each case breaks one link between the stable pointer and the installed
// bytes. The update must fail and leave the running binary in place.
func TestUpdate_StableRefusesUnverifiedRelease(t *testing.T) {
	good := buildArchive(t, platformBinary(), []byte("v0.2.0 binary"))
	asset := "releases/v0.2.0/" + platformAsset("v0.2.0")
	sums := "releases/v0.2.0/checksums.txt"

	cases := []struct {
		name   string
		mutate func(map[string]cdnObject)
	}{
		{"pointer is not a stable tag", func(o map[string]cdnObject) {
			o["stable"] = cdnObject{body: []byte("v0.2.0-rc.1")}
			for k, v := range releaseObjects("v0.2.0-rc.1", good) {
				o[k] = v
			}
		}},
		{"pointer redirects", func(o map[string]cdnObject) {
			o["stable"] = cdnObject{status: http.StatusFound, location: "/cli/elsewhere"}
			o["elsewhere"] = cdnObject{body: []byte("v0.2.0")}
		}},
		{"no checksums entry for this platform", func(o map[string]cdnObject) {
			o[sums] = cdnObject{body: []byte(sha256Hex(good) + "  heygen_v0.2.0_plan9_amd64.tar.gz\n")}
		}},
		{"checksum mismatch", func(o map[string]cdnObject) {
			o[asset] = cdnObject{body: buildArchive(t, platformBinary(), []byte("other binary"))}
		}},
		{"binary not at the archive root", func(o map[string]cdnObject) {
			nested := buildArchive(t, "bin/"+platformBinary(), []byte("v0.2.0 binary"))
			for k, v := range releaseObjects("v0.2.0", nested) {
				o[k] = v
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			objects := releaseObjects("v0.2.0", good)
			objects["stable"] = cdnObject{body: []byte("v0.2.0")}
			tc.mutate(objects)
			_, binPath := setupCDNUpdate(t, objects)

			res := runUpdateRoot(t, "v0.1.0", "update")
			if res.ExitCode != clierrors.ExitGeneral {
				t.Fatalf("ExitCode = %d, want %d\nstdout: %s\nstderr: %s", res.ExitCode, clierrors.ExitGeneral, res.Stdout, res.Stderr)
			}
			if bin := readBinary(t, binPath); bin != installedBinary {
				t.Errorf("binary was replaced with %q", bin)
			}
		})
	}
}

func TestUpdate_VersionResolvesItsCDNFolder(t *testing.T) {
	archive := buildArchive(t, platformBinary(), []byte("v0.1.5 binary"))
	sums := "releases/v0.1.5/checksums.txt"

	cases := []struct {
		name      string
		version   string
		objects   map[string]cdnObject
		installed string
	}{
		{name: "published version", version: "v0.1.5", objects: releaseObjects("v0.1.5", archive), installed: "v0.1.5 binary"},
		{name: "missing version answers 403", version: "v0.1.5", objects: map[string]cdnObject{}},
		{name: "missing version answers 404", version: "v0.1.5", objects: map[string]cdnObject{sums: {status: http.StatusNotFound}}},
		{name: "not a stable tag", version: "v0.1.5-rc.1", objects: releaseObjects("v0.1.5-rc.1", archive)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cdn, binPath := setupCDNUpdate(t, tc.objects)

			res := runUpdateRoot(t, "v0.2.0", "update", "--version", tc.version)
			if tc.installed != "" {
				if res.ExitCode != 0 {
					t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
				}
				if bin := readBinary(t, binPath); bin != tc.installed {
					t.Errorf("installed binary = %q, want %q", bin, tc.installed)
				}
				return
			}
			if res.ExitCode != clierrors.ExitGeneral || !strings.Contains(res.Stderr, "was not found") {
				t.Fatalf("ExitCode = %d, want %d with a not-found error\nstderr: %s", res.ExitCode, clierrors.ExitGeneral, res.Stderr)
			}
			if bin := readBinary(t, binPath); bin != installedBinary {
				t.Errorf("binary was replaced with %q", bin)
			}
			if strings.Contains(tc.version, "-") && len(cdn.requested()) != 0 {
				t.Errorf("requested %v; a version that is not a stable tag must never reach a URL", cdn.requested())
			}
		})
	}
}

func TestReleaseUpdater_DevChannelUsesGitHub(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token")
	u, err := newReleaseUpdater(true)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := u.(*selfUpdater); !ok {
		t.Errorf("dev channel updater = %T, want the GitHub release updater", u)
	}
}

func TestUpdateCheck_AgreesWithNoticeOnStableSource(t *testing.T) {
	setupCDNUpdate(t, map[string]cdnObject{"stable": {body: []byte("v0.3.0\n")}})

	res := runUpdateRoot(t, "v0.1.0", "update", "--check")
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}
	var check updateCheckResponse
	if err := json.Unmarshal([]byte(res.Stdout), &check); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %s", err, res.Stdout)
	}
	notice, err := updateFetchLatest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if check.Latest != "v0.3.0" || notice != check.Latest {
		t.Errorf("--check latest = %q, notice latest = %q; both want the stable pointer's v0.3.0", check.Latest, notice)
	}
}
