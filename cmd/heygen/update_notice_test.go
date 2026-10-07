package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/heygen-com/heygen-cli/internal/output"
)

var updateTestNow = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

// setupUpdateNotice isolates a test from the real environment: CI runners set
// CI and GITHUB_ACTIONS, which would turn the check off for every case.
func setupUpdateNotice(t *testing.T) *atomic.Int32 {
	t.Helper()
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	for _, name := range []string{"CI", "GITHUB_ACTIONS", "HEYGEN_NONINTERACTIVE", "HEYGEN_NO_UPDATE_CHECK"} {
		t.Setenv(name, "")
	}
	origNow, origFetch, origSpawn := updateNow, updateFetchLatest, updateSpawnWorker
	origExe, origEval := updateExecutablePath, updateEvalSymlinks
	t.Cleanup(func() {
		updateNow, updateFetchLatest, updateSpawnWorker = origNow, origFetch, origSpawn
		updateExecutablePath, updateEvalSymlinks = origExe, origEval
	})
	updateNow = func() time.Time { return updateTestNow }
	updateExecutablePath = func() (string, error) { return "/usr/local/bin/heygen", nil }
	updateEvalSymlinks = func(path string) (string, error) { return path, nil }
	updateFetchLatest = func(context.Context) (string, error) { return "v0.9.1", nil }
	var spawns atomic.Int32
	updateSpawnWorker = func() error {
		spawns.Add(1)
		return nil
	}
	return &spawns
}

// runUpdateNotice drives one invocation's parent side and returns stderr.
func runUpdateNotice(t *testing.T, version string) string {
	t.Helper()
	n := startUpdateCheck([]string{"video", "list"}, version)
	var stderr bytes.Buffer
	n.finish(output.NewJSONFormatter(io.Discard, &stderr))
	return stderr.String()
}

func seedUpdateCache(t *testing.T, c updateCheckCache) {
	t.Helper()
	if err := writeUpdateCache(c); err != nil {
		t.Fatalf("writeUpdateCache: %v", err)
	}
}

func assertUpdateNotice(t *testing.T, stderr, latest, current string) {
	t.Helper()
	var envelope map[string]map[string]string
	if err := json.Unmarshal([]byte(stderr), &envelope); err != nil {
		t.Fatalf("stderr is not a single notice envelope: %v (%q)", err, stderr)
	}
	if got := envelope["notice"]["code"]; got != updateNoticeCode {
		t.Errorf("notice.code = %q, want %q", got, updateNoticeCode)
	}
	if msg := envelope["notice"]["message"]; !strings.Contains(msg, latest) || !strings.Contains(msg, current) {
		t.Errorf("notice.message = %q, want both %s and %s", msg, latest, current)
	}
}

func TestSkipsUpdateCheck(t *testing.T) {
	cases := []struct {
		args []string
		skip bool
	}{
		{nil, true},
		{[]string{"__complete", "video", ""}, true},
		{[]string{"completion", "zsh"}, true},
		{[]string{"help", "video"}, true},
		{[]string{"update", "--check"}, true},
		{[]string{"--version"}, true},
		{[]string{"video", "list", "--help"}, true},
		{[]string{"video", "create", "--request-schema=true"}, true},
		{[]string{"video", "get", "--response-schema"}, true},
		{[]string{"video", "list"}, false},
		{[]string{"--human", "video", "list"}, false},
		// --headers consumes the next arg, so "update" here is its value.
		{[]string{"--headers", "update", "video", "list"}, false},
	}
	for _, tc := range cases {
		if got := skipsUpdateCheck(tc.args); got != tc.skip {
			t.Errorf("skipsUpdateCheck(%q) = %v, want %v", tc.args, got, tc.skip)
		}
	}
}

func TestUpdateCheckEnabled(t *testing.T) {
	cases := []struct {
		name    string
		env     map[string]string
		config  string
		enabled bool
	}{
		{name: "default", enabled: true},
		{name: "CI=false is not CI", env: map[string]string{"CI": "false"}, enabled: true},
		{name: "CI", env: map[string]string{"CI": "true"}},
		{name: "GitHub Actions", env: map[string]string{"GITHUB_ACTIONS": "true"}},
		{name: "noninteractive", env: map[string]string{"HEYGEN_NONINTERACTIVE": "1"}},
		{name: "env opt-out", env: map[string]string{"HEYGEN_NO_UPDATE_CHECK": "1"}},
		{name: "config opt-out", config: "update_check = false\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setupUpdateNotice(t)
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			if tc.config != "" {
				path := filepath.Join(os.Getenv("HEYGEN_CONFIG_DIR"), "config.toml")
				if err := os.WriteFile(path, []byte(tc.config), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if got := updateCheckEnabled(); got != tc.enabled {
				t.Errorf("updateCheckEnabled() = %v, want %v", got, tc.enabled)
			}
		})
	}
}

func TestStartUpdateCheck_OnlyStableReleaseBuildsCheck(t *testing.T) {
	for _, version := range []string{"dev", "v0.9.0-6-gabc1234-dirty", "v0.9.0-dev.202610070000"} {
		t.Run(version, func(t *testing.T) {
			spawns := setupUpdateNotice(t)
			if n := startUpdateCheck([]string{"video", "list"}, version); n != nil {
				t.Errorf("startUpdateCheck(%q) returned a notifier, want nil", version)
			}
			if spawns.Load() != 0 {
				t.Errorf("spawned %d workers, want 0", spawns.Load())
			}
		})
	}
}

func TestUpdateCheck_DueCheckRecordsAttemptAndSpawnsWorker(t *testing.T) {
	spawns := setupUpdateNotice(t)

	if stderr := runUpdateNotice(t, "v0.9.0"); stderr != "" {
		t.Errorf("stderr = %q, want nothing before any check has succeeded", stderr)
	}
	if spawns.Load() != 1 {
		t.Errorf("spawned %d workers, want 1", spawns.Load())
	}
	if c := readUpdateCache(); !c.LastAttempt.Equal(updateTestNow) {
		t.Errorf("last_attempt = %v, want %v recorded before spawning", c.LastAttempt, updateTestNow)
	}
}

func TestUpdateCheck_FreshCacheNotifiesWithoutSpawning(t *testing.T) {
	spawns := setupUpdateNotice(t)
	seedUpdateCache(t, updateCheckCache{Latest: "v0.9.1", CheckedAt: updateTestNow.Add(-time.Hour)})

	stderr := runUpdateNotice(t, "v0.9.0")

	if spawns.Load() != 0 {
		t.Errorf("spawned %d workers, want 0 while the cache is fresh", spawns.Load())
	}
	assertUpdateNotice(t, stderr, "v0.9.1", "v0.9.0")
}

func TestUpdateCheck_NoticeInterval(t *testing.T) {
	cases := []struct {
		name       string
		latest     string
		notifiedAt time.Time
		want       bool
	}{
		{"newer, never notified", "v0.9.1", time.Time{}, true},
		{"newer, notified just inside the interval", "v0.9.1", updateTestNow.Add(-updateNoticeInterval + time.Second), false},
		{"newer, notified a full interval ago", "v0.9.1", updateTestNow.Add(-updateNoticeInterval), true},
		{"same version", "v0.9.0", time.Time{}, false},
		{"unknown latest", "", time.Time{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setupUpdateNotice(t)
			seedUpdateCache(t, updateCheckCache{Latest: tc.latest, CheckedAt: updateTestNow, NotifiedAt: tc.notifiedAt})
			stderr := runUpdateNotice(t, "v0.9.0")
			if got := stderr != ""; got != tc.want {
				t.Errorf("notice emitted = %v, want %v (stderr %q)", got, tc.want, stderr)
			}
		})
	}
}

func TestCheckDue(t *testing.T) {
	cases := []struct {
		name string
		c    updateCheckCache
		want bool
	}{
		{"never checked", updateCheckCache{}, true},
		{"checked just inside the interval", updateCheckCache{CheckedAt: updateTestNow.Add(-updateCheckInterval + time.Second)}, false},
		{"checked a full interval ago", updateCheckCache{CheckedAt: updateTestNow.Add(-updateCheckInterval)}, true},
		{"one failure, inside its backoff", updateCheckCache{Failures: 1, LastAttempt: updateTestNow.Add(-2*updateRetryBase + time.Second)}, false},
		{"one failure, backoff elapsed", updateCheckCache{Failures: 1, LastAttempt: updateTestNow.Add(-2 * updateRetryBase)}, true},
		{"many failures cap at the check interval", updateCheckCache{Failures: 40, LastAttempt: updateTestNow.Add(-updateCheckInterval)}, true},
		{"timestamps in the future count as elapsed", updateCheckCache{CheckedAt: updateTestNow.Add(time.Hour), LastAttempt: updateTestNow.Add(time.Hour)}, true},
	}
	for _, tc := range cases {
		if got := tc.c.checkDue(updateTestNow); got != tc.want {
			t.Errorf("%s: checkDue = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// The worker records its outcome on top of what the parent left: a success
// resets the backoff, a failure extends it, and the parent's attempt stays.
func TestUpdateCheckWorker_RecordsOutcome(t *testing.T) {
	attempt := updateTestNow.Add(-time.Minute)
	cases := []struct {
		name  string
		fetch func(context.Context) (string, error)
		want  updateCheckCache
	}{
		{
			name:  "success",
			fetch: func(context.Context) (string, error) { return "v0.9.1", nil },
			want:  updateCheckCache{Latest: "v0.9.1", CheckedAt: updateTestNow, LastAttempt: attempt},
		},
		{
			name:  "failure",
			fetch: func(context.Context) (string, error) { return "", errors.New("offline") },
			want:  updateCheckCache{Latest: "v0.9.0", LastAttempt: attempt, Failures: 3},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setupUpdateNotice(t)
			seedUpdateCache(t, updateCheckCache{Latest: "v0.9.0", LastAttempt: attempt, Failures: 2})
			updateFetchLatest = tc.fetch

			runUpdateCheckWorker()

			got := readUpdateCache()
			got.Schema = 0
			if got != tc.want {
				t.Errorf("cache = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// A parent holds the lock only briefly, so the worker waits rather than drop
// a result it already paid for.
func TestUpdateCheckWorker_WaitsForBusyLock(t *testing.T) {
	setupUpdateNotice(t)
	unlock, err := lockUpdateCache()
	if err != nil {
		t.Fatalf("lockUpdateCache: %v", err)
	}
	time.AfterFunc(200*time.Millisecond, unlock)

	runUpdateCheckWorker()

	if c := readUpdateCache(); c.Latest != "v0.9.1" {
		t.Errorf("cache = %+v, want the worker's result recorded once the lock freed", c)
	}
}

func TestReadUpdateCache_UnusableFileIsAbsent(t *testing.T) {
	cases := map[string][]byte{
		"not json":     []byte("{"),
		"other schema": []byte(`{"schema": 99, "latest": "v9.9.9"}`),
		// Valid JSON once truncated to the read limit, so only the size bound rejects it.
		"oversized": append([]byte(`{"schema": 1, "latest": "v9.9.9"}`), bytes.Repeat([]byte(" "), updateCacheMaxBytes)...),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			setupUpdateNotice(t)
			if err := os.WriteFile(updateCachePath(), data, 0o600); err != nil {
				t.Fatal(err)
			}
			if c := readUpdateCache(); c != (updateCheckCache{}) {
				t.Errorf("readUpdateCache() = %+v, want the zero cache", c)
			}
		})
	}
}

// Without a writable cache nothing records attempts or notices, so checking
// would fetch, and notify, on every invocation.
func TestUpdateCheck_UnwritableConfigDirStaysOff(t *testing.T) {
	spawns := setupUpdateNotice(t)
	notADir := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(notADir, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HEYGEN_CONFIG_DIR", notADir)

	if stderr := runUpdateNotice(t, "v0.9.0"); stderr != "" {
		t.Errorf("stderr = %q, want nothing", stderr)
	}
	if spawns.Load() != 0 {
		t.Errorf("spawned %d workers, want 0", spawns.Load())
	}
}

// The attempt is what stops the next invocation spawning again, so a worker
// must not start unless it was recorded. A directory at the cache path lets
// the lock succeed while the write's rename fails.
func TestUpdateCheck_UnrecordedAttemptDoesNotSpawn(t *testing.T) {
	spawns := setupUpdateNotice(t)
	if err := os.Mkdir(updateCachePath(), 0o700); err != nil {
		t.Fatal(err)
	}

	runUpdateNotice(t, "v0.9.0")
	if spawns.Load() != 0 {
		t.Errorf("spawned %d workers, want 0 when the attempt could not be recorded", spawns.Load())
	}
}

// A notice whose timestamp cannot be saved would repeat on every invocation.
// A read-only config dir lets the lock and the read succeed while the write
// fails, so finish reaches the emit decision.
func TestUpdateCheck_NoticeRequiresRecordedTimestamp(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs POSIX permissions enforced on the caller")
	}
	setupUpdateNotice(t)
	seedUpdateCache(t, updateCheckCache{Latest: "v0.9.1", CheckedAt: updateTestNow})
	unlock, err := lockUpdateCache() // creates the lock file while the dir is writable
	if err != nil {
		t.Fatal(err)
	}
	unlock()
	dir := os.Getenv("HEYGEN_CONFIG_DIR")
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	if stderr := runUpdateNotice(t, "v0.9.0"); stderr != "" {
		t.Errorf("stderr = %q, want no notice when it cannot be recorded", stderr)
	}
}

// A concurrent invocation can record its stamp after this one first read the
// clock but before this one takes the lock. Read with the earlier clock, that
// stamp looks like a clock set back, which counts as elapsed, so this run
// would spawn a second worker or print a second notice.
func TestUpdateCheck_ConcurrentStampBeforeLockIsHonored(t *testing.T) {
	cases := []struct {
		name string
		seed func(stamp time.Time) updateCheckCache
		run  func() (stderr string)
	}{
		{
			name: "worker spawn",
			seed: func(stamp time.Time) updateCheckCache { return updateCheckCache{LastAttempt: stamp} },
			run: func() string {
				startUpdateCheck([]string{"video", "list"}, "v0.9.0")
				return ""
			},
		},
		{
			name: "notice",
			seed: func(stamp time.Time) updateCheckCache {
				return updateCheckCache{Latest: "v0.9.1", CheckedAt: updateTestNow, NotifiedAt: stamp}
			},
			run: func() string {
				var stderr bytes.Buffer
				(&updateNotifier{current: "v0.9.0"}).finish(output.NewJSONFormatter(io.Discard, &stderr))
				return stderr.String()
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spawns := setupUpdateNotice(t)
			clock := updateTestNow
			updateNow = func() time.Time {
				clock = clock.Add(time.Millisecond)
				return clock
			}
			// Between the first read (+1ms) and the read under the lock (+2ms).
			seedUpdateCache(t, tc.seed(updateTestNow.Add(1500*time.Microsecond)))

			if stderr := tc.run(); stderr != "" {
				t.Errorf("stderr = %q, want no second notice", stderr)
			}
			if spawns.Load() != 0 {
				t.Errorf("spawned %d workers, want no second worker", spawns.Load())
			}
		})
	}
}

// Another invocation holding the lock means this one neither checks nor
// notifies; waiting for it would delay the command. The cases reach the lock
// from each side: a due check takes it in startUpdateCheck, a due notice in
// finish.
func TestUpdateCheck_BusyLockSkipsCacheWork(t *testing.T) {
	cases := map[string]updateCheckCache{
		"check due":  {},
		"notice due": {Latest: "v0.9.1", CheckedAt: updateTestNow},
	}
	for name, seed := range cases {
		t.Run(name, func(t *testing.T) {
			spawns := setupUpdateNotice(t)
			if seed != (updateCheckCache{}) {
				seedUpdateCache(t, seed)
			}
			unlock, err := lockUpdateCache()
			if err != nil {
				t.Fatalf("lockUpdateCache: %v", err)
			}
			defer unlock()

			if stderr := runUpdateNotice(t, "v0.9.0"); stderr != "" {
				t.Errorf("stderr = %q, want nothing while the lock is held", stderr)
			}
			if spawns.Load() != 0 {
				t.Errorf("spawned %d workers, want 0 while the lock is held", spawns.Load())
			}
		})
	}
}

// The lock is held by an open file, not by the file existing, so a lock file
// left behind blocks nothing, and finish releases what it took.
func TestUpdateCheck_LockFileWithoutHolderDoesNotBlock(t *testing.T) {
	setupUpdateNotice(t)
	seedUpdateCache(t, updateCheckCache{Latest: "v0.9.1", CheckedAt: updateTestNow})
	if err := os.WriteFile(updateCachePath()+".lock", nil, 0o600); err != nil {
		t.Fatal(err)
	}

	assertUpdateNotice(t, runUpdateNotice(t, "v0.9.0"), "v0.9.1", "v0.9.0")
	unlock, err := lockUpdateCache()
	if err != nil {
		t.Fatalf("lock still held after finish: %v", err)
	}
	unlock()
}

const (
	detachTestRun     = "-test.run=^TestStartDetached_DoesNotHoldCallerStreams$"
	detachHelperEnv   = "HEYGEN_TEST_DETACH_HELPER"
	detachExitArg     = "detach-exit"
	detachCallerLimit = 5 * time.Second
)

// Pins startDetached's descriptor contract. This test is the caller, the test
// binary rerun as a helper starts a ~10 s child and exits at once. The child
// is a system command rather than the test binary because Windows cannot
// delete a running executable, which fails go test's cleanup. exec.Cmd.Wait returns only once
// the helper's stdout and stderr reach EOF, and on Unix the helper also gets
// an extra pipe as fd 3 that is not close-on-exec, the way a harness's IPC
// channel arrives. Both reach EOF promptly only if the child holds neither.
func TestStartDetached_DoesNotHoldCallerStreams(t *testing.T) {
	if os.Getenv(detachHelperEnv) != "" {
		name, args := "sleep", []string{"10"}
		if runtime.GOOS == "windows" {
			name, args = "ping", []string{"-n", "11", "127.0.0.1"}
		}
		if err := startDetached(name, args...); err != nil {
			os.Exit(3)
		}
		os.Exit(0)
	}

	cmd := exec.Command(os.Args[0], detachTestRun)
	cmd.Env = append(os.Environ(), detachHelperEnv+"=1")
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	extraEOF := make(chan struct{})
	if runtime.GOOS != "windows" {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()
		cmd.ExtraFiles = []*os.File{w}
		go func() {
			_, _ = io.Copy(io.Discard, r)
			close(extraEOF)
		}()
		defer w.Close()
	} else {
		close(extraEOF)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	for _, f := range cmd.ExtraFiles {
		_ = f.Close()
	}
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	deadline := time.After(detachCallerLimit)
	select {
	case err := <-waited:
		if err != nil {
			t.Fatalf("helper failed to start the detached child: %v\n%s", err, out.String())
		}
	case <-deadline:
		t.Fatal("caller's stdout or stderr stayed open: the detached child inherited them")
	}
	select {
	case <-extraEOF:
	case <-deadline:
		t.Fatal("caller's extra descriptor stayed open: the detached child inherited it")
	}
}

// A finished worker must not linger as a zombie under a parent that is still
// running, such as a long `video create --wait`.
func TestStartDetached_ReapsChild(t *testing.T) {
	if slices.Contains(os.Args, detachExitArg) {
		os.Exit(0)
	}
	if runtime.GOOS != "linux" {
		t.Skip("reads process state from /proc")
	}
	if err := startDetached(os.Args[0], "-test.run=^TestStartDetached_ReapsChild$", "--", detachExitArg); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Second)
	if zombies := zombieChildren(t); len(zombies) > 0 {
		t.Errorf("zombie children %v remain after the worker exited", zombies)
	}
}

func zombieChildren(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir("/proc")
	if err != nil {
		t.Fatal(err)
	}
	self := strconv.Itoa(os.Getpid())
	var zombies []string
	for _, e := range entries {
		stat, err := os.ReadFile(filepath.Join("/proc", e.Name(), "stat"))
		if err != nil {
			continue
		}
		// pid (comm) state ppid ...; comm may contain spaces, so split after it.
		fields := strings.Fields(string(stat[bytes.LastIndexByte(stat, ')')+1:]))
		if len(fields) > 1 && fields[0] == "Z" && fields[1] == self {
			zombies = append(zombies, e.Name())
		}
	}
	return zombies
}

func TestWriteUpdateCache_OwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits")
	}
	setupUpdateNotice(t)
	seedUpdateCache(t, updateCheckCache{Latest: "v0.9.1"})
	info, err := os.Stat(updateCachePath())
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("cache mode = %o, want 600", mode)
	}
}

func TestUpdateNoticeMessage_PackageManagerInstall(t *testing.T) {
	setupUpdateNotice(t)
	updateExecutablePath = func() (string, error) { return "/opt/homebrew/bin/heygen", nil }

	if msg := updateNoticeMessage("v0.9.1", "v0.9.0"); !strings.Contains(msg, "brew upgrade heygen") {
		t.Errorf("message = %q, want the homebrew upgrade command", msg)
	}
}

func TestFetchStablePointer(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"stable tag", http.StatusOK, "v0.9.1\n", "v0.9.1"},
		{"dev tag", http.StatusOK, "v0.9.1-dev.202610070000", ""},
		{"not a tag", http.StatusOK, "<html>", ""},
		{"oversized", http.StatusOK, "v0.9.1" + strings.Repeat(" ", updatePointerMaxBytes), ""},
		{"server error", http.StatusInternalServerError, "v0.9.1", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()
			orig := updateStablePointerURL
			t.Cleanup(func() { updateStablePointerURL = orig })
			updateStablePointerURL = srv.URL

			got, err := fetchStablePointer(context.Background())
			if tc.want == "" {
				if err == nil {
					t.Errorf("fetchStablePointer() = %q, want an error", got)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Errorf("fetchStablePointer() = %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}
