package analytics

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// tree lists every entry under root with its contents, so a test can prove nothing there was read into use,
// created or rewritten.
func tree(t *testing.T, root string) map[string]string {
	t.Helper()
	entries := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if d.IsDir() {
			entries[rel] = "dir"
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		entries[rel] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func TestNoAbsoluteHomeLeavesTheWorkingDirectoryAlone(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	for _, home := range []string{"", ".", filepath.Join("relative", "home")} {
		cwd := t.TempDir()
		t.Chdir(cwd)
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", home)
		planted := filepath.Join(cwd, home, ".hyperframes", "config.json")
		if err := os.MkdirAll(filepath.Dir(planted), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(planted, []byte(`{"anonymousId":"planted"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		before := tree(t, cwd)

		client := newWithCapture("v1", &stubCaptureClient{})

		if client.distinctID == "planted" {
			t.Fatalf("home %q: adopted the analytics id planted in the working directory", home)
		}
		after := tree(t, cwd)
		if len(after) != len(before) {
			t.Fatalf("home %q: the working directory went from %d to %d entries", home, len(before), len(after))
		}
		for path, content := range before {
			if after[path] != content {
				t.Fatalf("home %q: %s changed from %q to %q", home, path, content, after[path])
			}
		}
	}
}

func TestSharedConfigWriteReplacesALinkInsteadOfWritingThroughIt(t *testing.T) {
	home := t.TempDir()
	setHomeDirForTest(t, home)
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	victim := filepath.Join(t.TempDir(), "victim.txt")
	if err := os.WriteFile(victim, []byte("keep me\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".hyperframes", "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, path); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}

	id := distinctID()

	if data, _ := os.ReadFile(victim); string(data) != "keep me\n" {
		t.Fatalf("the link's target was rewritten: %q", data)
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("the config path is not a regular file after the write: %v, %v", info, err)
	}
	if got := readSharedConfig()["anonymousId"]; got != id {
		t.Fatalf("anonymousId = %v, want %q", got, id)
	}
}
