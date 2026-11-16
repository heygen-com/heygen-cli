package paths

import (
	"errors"
	"path/filepath"
	"testing"
)

func setHome(t *testing.T, home string) {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
}

func TestConfigDirIsNeverTheWorkingDirectory(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", "")
	for _, home := range []string{"", ".", filepath.Join("relative", "home")} {
		setHome(t, home)
		if dir, err := ConfigDir(); !errors.Is(err, ErrNoConfigDir) {
			t.Fatalf("home %q: got %q, %v; want ErrNoConfigDir", home, dir, err)
		}
	}
}

func TestConfigDirUsesHomeOrTheOverride(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)
	t.Setenv("HEYGEN_CONFIG_DIR", "")
	if dir, err := ConfigDir(); err != nil || dir != filepath.Join(home, ".heygen") {
		t.Fatalf("got %q, %v", dir, err)
	}

	setHome(t, "")
	t.Setenv("HEYGEN_CONFIG_DIR", "/elsewhere")
	if dir, err := ConfigDir(); err != nil || dir != "/elsewhere" {
		t.Fatalf("got %q, %v", dir, err)
	}
}
