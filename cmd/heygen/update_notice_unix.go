//go:build unix

package main

import (
	"errors"
	"os"
	"runtime"
	"strconv"
	"syscall"

	"golang.org/x/sys/unix"
)

func tryLockFile(f *os.File) error {
	err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) {
		return errUpdateCacheBusy
	}
	return err
}

func unlockFile(f *os.File) {
	_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
}

// A new session keeps the worker out of the terminal's process group, so a
// Ctrl-C or hangup aimed at the caller does not reach it.
func detachedProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}

// closeInheritedOnExec marks every descriptor above stderr close-on-exec. Go
// opens its own that way, so the rest were inherited from the caller; it is a
// snapshot, sound only while heygen opens no raw descriptors (no cgo). The flag
// belongs to this process's descriptor table and only stops later children,
// which heygen never relies on passing them to, from inheriting them.
func closeInheritedOnExec() error {
	dir := "/dev/fd"
	if runtime.GOOS == "linux" {
		dir = "/proc/self/fd"
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if fd, err := strconv.Atoi(e.Name()); err == nil && fd > 2 {
			unix.CloseOnExec(fd)
		}
	}
	return nil
}
