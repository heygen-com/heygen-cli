package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/heygen-com/heygen-cli/internal/config"
	"github.com/heygen-com/heygen-cli/internal/output"
	"github.com/heygen-com/heygen-cli/internal/paths"
)

const (
	updateNoticeCode = "cli_update_available"

	updateCheckInterval  = 24 * time.Hour
	updateNoticeInterval = 24 * time.Hour
	updateRetryBase      = time.Hour
	updateFetchTimeout   = 3 * time.Second

	updateCacheSchema     = 1
	updateCacheMaxBytes   = 4 << 10
	updatePointerMaxBytes = 64
)

var (
	// updateStablePointerURL is the file install.sh resolves "latest" from, so
	// the notice can never name a release the installer would not serve.
	updateStablePointerURL = "https://static.heygen.ai/cli/stable"
	updateNow              = time.Now
	updateFetchLatest      = fetchStablePointer
)

// Invocations matching these never check or notify. __complete runs on every
// tab press, where a network call is unacceptable.
var (
	updateCheckSkippedCommands = map[string]bool{
		"__complete": true, "__completeNoDesc": true, "completion": true, "help": true, "update": true,
	}
	updateCheckSkippedFlags = map[string]bool{
		"--version": true, "-v": true, "--help": true, "-h": true,
		"--request-schema": true, "--response-schema": true,
	}
)

type updateCheckCache struct {
	Schema      int       `json:"schema"`
	Latest      string    `json:"latest,omitempty"`
	CheckedAt   time.Time `json:"checked_at"`
	LastAttempt time.Time `json:"last_attempt"`
	Failures    int       `json:"failures"`
	NotifiedAt  time.Time `json:"notified_at"`
}

type updateFetchResult struct {
	latest string
	err    error
}

// A nil *updateNotifier means the check is off for this invocation; finish
// does nothing on it.
type updateNotifier struct {
	current string
	cache   updateCheckCache
	result  chan updateFetchResult
}

// startUpdateCheck starts a due check in the background, so it races the
// command rather than delaying it.
func startUpdateCheck(args []string, version string) *updateNotifier {
	if !updateCheckEnabled() || skipsUpdateCheck(args) {
		return nil
	}
	current, err := validateCurrentVersion(version)
	if err != nil || updateChannel(current) != "stable" {
		return nil
	}
	n := &updateNotifier{current: current, cache: readUpdateCache()}
	now := updateNow()
	if !n.cache.checkDue(now) {
		return n
	}
	unlock, err := lockUpdateCache()
	if err != nil {
		return nil
	}
	defer unlock()
	// Re-read under the lock: another invocation may have checked meanwhile.
	n.cache = readUpdateCache()
	if !n.cache.checkDue(now) {
		return n
	}
	// The attempt is recorded before fetching so an abandoned or failed check
	// still counts toward backoff. If it cannot be recorded, nothing could stop
	// the next invocation fetching again, so the check stays off.
	n.cache.LastAttempt = now
	if err := writeUpdateCache(n.cache); err != nil {
		return nil
	}
	n.result = make(chan updateFetchResult, 1)
	fetch := updateFetchLatest
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), updateFetchTimeout)
		defer cancel()
		latest, err := fetch(ctx)
		n.result <- updateFetchResult{latest: latest, err: err}
	}()
	return n
}

// finish never waits: a check still in flight is abandoned, and the notice is
// decided from whatever the cache already knew.
func (n *updateNotifier) finish(formatter output.Formatter) {
	if n == nil {
		return
	}
	var result *updateFetchResult
	select {
	case r := <-n.result:
		result = &r
	default:
	}
	now := updateNow()
	if result == nil && !n.cache.noticeDue(n.current, now) {
		return
	}
	unlock, err := lockUpdateCache()
	if err != nil {
		return
	}
	defer unlock()
	// Apply to the cache as it is now on disk, not as startUpdateCheck read it,
	// so a concurrent invocation's check or notice is not overwritten.
	c := readUpdateCache()
	if result != nil {
		c.apply(*result, now)
	}
	notify := c.noticeDue(n.current, now)
	if notify {
		c.NotifiedAt = now
	}
	// Emitted only once recorded, or an unwritable config dir would repeat the
	// notice on every invocation.
	if writeUpdateCache(c) == nil && notify {
		formatter.Notice(updateNoticeCode, updateNoticeMessage(c.Latest, n.current))
	}
}

func (c *updateCheckCache) apply(r updateFetchResult, now time.Time) {
	if r.err != nil {
		c.Failures++
		return
	}
	c.Latest = r.latest
	c.CheckedAt = now
	c.Failures = 0
}

// checkDue treats a timestamp in the future as elapsed, so a clock set back
// cannot suppress checks until it catches up.
func (c updateCheckCache) checkDue(now time.Time) bool {
	if !c.CheckedAt.After(now) && now.Sub(c.CheckedAt) < updateCheckInterval {
		return false
	}
	backoff := min(updateRetryBase<<min(c.Failures, 5), updateCheckInterval)
	return c.LastAttempt.After(now) || now.Sub(c.LastAttempt) >= backoff
}

func (c updateCheckCache) noticeDue(current string, now time.Time) bool {
	if !isVersionGreater(c.Latest, current) {
		return false
	}
	return c.NotifiedAt.After(now) || now.Sub(c.NotifiedAt) >= updateNoticeInterval
}

func updateNoticeMessage(latest, current string) string {
	how := "Run `heygen update` to upgrade."
	if method, _, err := detectInstallMethod(); err == nil {
		switch method {
		case "homebrew":
			how = "Run `brew upgrade heygen` to upgrade."
		case "npm":
			how = "Upgrade it through your package manager."
		}
	}
	return fmt.Sprintf("heygen %s is available; you have %s. %s", latest, current, how)
}

// updateCheckEnabled is off in CI because a job gets a fresh HOME, so the
// cache never persists and every job's first command would fetch. Agent
// shells are deliberately not CI: they are the audience for the notice.
func updateCheckEnabled() bool {
	if envTruthy("CI") || envTruthy("GITHUB_ACTIONS") || envTruthy("HEYGEN_NONINTERACTIVE") {
		return false
	}
	if os.Getenv("HEYGEN_NO_UPDATE_CHECK") != "" {
		return false
	}
	fp := &config.FileProvider{}
	val, found, err := fp.Get(config.KeyUpdateCheck)
	return !(err == nil && found && val == "false")
}

// skipsUpdateCheck reads raw args because the decision is made before Cobra
// parses them, and Cobra registers __complete only during execution.
func skipsUpdateCheck(args []string) bool {
	first := ""
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			break
		}
		name, _, _ := strings.Cut(arg, "=")
		if updateCheckSkippedFlags[name] {
			return true
		}
		if arg == "--headers" { // the root's only persistent flag that takes a value
			i++
			continue
		}
		if first == "" && !strings.HasPrefix(arg, "-") {
			first = arg
		}
	}
	return first == "" || updateCheckSkippedCommands[first]
}

func fetchStablePointer(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, updateStablePointerURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("stable pointer returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, updatePointerMaxBytes+1))
	if err != nil {
		return "", err
	}
	if len(body) > updatePointerMaxBytes {
		return "", fmt.Errorf("stable pointer exceeds %d bytes", updatePointerMaxBytes)
	}
	latest := strings.TrimSpace(string(body))
	if !releaseTaggedVersion.MatchString(latest) || updateChannel(latest) != "stable" {
		return "", fmt.Errorf("stable pointer %q is not a stable release tag", latest)
	}
	return latest, nil
}

var errUpdateCacheBusy = errors.New("update check cache is locked by another invocation")

// lockUpdateCache never waits: a busy lock means another invocation is already
// updating the cache, so this one skips its update rather than delay its
// command. The lock is an OS advisory lock on an open file, which the kernel
// releases if the process dies, so the lock file is never deleted: unlinking
// it would let a second process lock a fresh inode while the first still holds
// the old one.
func lockUpdateCache() (func(), error) {
	if err := os.MkdirAll(paths.ConfigDir(), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(updateCachePath()+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := tryLockFile(f); err != nil {
		_ = f.Close()
		return nil, err
	}
	return func() {
		unlockFile(f)
		_ = f.Close()
	}, nil
}

func updateCachePath() string {
	return filepath.Join(paths.ConfigDir(), "update_check.json")
}

// readUpdateCache treats a missing, oversized, unparseable or other-schema
// file as absent, so a bad cache costs one check rather than breaking any.
func readUpdateCache() updateCheckCache {
	f, err := os.Open(updateCachePath())
	if err != nil {
		return updateCheckCache{}
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, updateCacheMaxBytes+1))
	if err != nil || len(data) > updateCacheMaxBytes {
		return updateCheckCache{}
	}
	var c updateCheckCache
	if json.Unmarshal(data, &c) != nil || c.Schema != updateCacheSchema {
		return updateCheckCache{}
	}
	return c
}

// writeUpdateCache replaces the file by rename, so an unlocked reader sees the
// old cache or the new one, never a partial write. Callers hold
// lockUpdateCache.
func writeUpdateCache(c updateCheckCache) error {
	c.Schema = updateCacheSchema
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	dir := paths.ConfigDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".update_check-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), updateCachePath())
}
