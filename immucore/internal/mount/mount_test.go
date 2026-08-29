package mount

import (
	"errors"
	"os"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestMountParsesFlagsAndPreservesDataOrder(t *testing.T) {
	originalMount := mountSyscall
	t.Cleanup(func() { mountSyscall = originalMount })

	var gotSource, gotTarget, gotType, gotData string
	var gotFlags uintptr
	mountSyscall = func(source, target, fsType string, flags uintptr, data string) error {
		gotSource, gotTarget, gotType = source, target, fsType
		gotFlags, gotData = flags, data
		return nil
	}

	m := Mount{
		Type:   "ext4",
		Source: "/dev/test",
		Options: []string{
			"ro", "nodev", "noexec", "nosuid", "sync", "dirsync",
			"noatime", "nodiratime", "relatime", "strictatime",
			"bind", "rbind", "remount", "unknown", "key=value",
		},
	}
	if err := m.Mount("/target"); err != nil {
		t.Fatalf("Mount() error = %v", err)
	}

	wantFlags := uintptr(unix.MS_RDONLY | unix.MS_NODEV | unix.MS_NOEXEC |
		unix.MS_NOSUID | unix.MS_SYNCHRONOUS | unix.MS_DIRSYNC |
		unix.MS_NOATIME | unix.MS_NODIRATIME | unix.MS_RELATIME |
		unix.MS_STRICTATIME | unix.MS_BIND | unix.MS_REC | unix.MS_REMOUNT)
	if gotSource != m.Source || gotTarget != "/target" || gotType != m.Type {
		t.Fatalf("mount arguments = (%q, %q, %q), want (%q, %q, %q)",
			gotSource, gotTarget, gotType, m.Source, "/target", m.Type)
	}
	if gotFlags != wantFlags {
		t.Errorf("mount flags = %#x, want %#x", gotFlags, wantFlags)
	}
	if gotData != "unknown,key=value" {
		t.Errorf("mount data = %q, want %q", gotData, "unknown,key=value")
	}
}

func TestMountClearsFlags(t *testing.T) {
	originalMount := mountSyscall
	t.Cleanup(func() { mountSyscall = originalMount })

	var gotFlags uintptr
	mountSyscall = func(_, _, _ string, flags uintptr, _ string) error {
		gotFlags = flags
		return nil
	}

	m := Mount{Options: []string{
		"ro", "rw", "nodev", "dev", "noexec", "exec", "nosuid", "suid",
		"sync", "async", "noatime", "atime", "nodiratime", "diratime",
		"relatime", "norelatime", "strictatime", "nostrictatime",
		"mand", "nomand", "defaults",
	}}
	if err := m.Mount("/target"); err != nil {
		t.Fatalf("Mount() error = %v", err)
	}
	if gotFlags != 0 {
		t.Errorf("mount flags = %#x, want 0", gotFlags)
	}
}

func TestMountRejectsPageSizedData(t *testing.T) {
	originalMount := mountSyscall
	t.Cleanup(func() { mountSyscall = originalMount })

	called := false
	mountSyscall = func(_, _, _ string, _ uintptr, _ string) error {
		called = true
		return nil
	}

	m := Mount{Options: []string{strings.Repeat("x", os.Getpagesize())}}
	err := m.Mount("/target")
	if err == nil {
		t.Fatal("Mount() error = nil, want mount data size error")
	}
	if called {
		t.Fatal("mount syscall called for page-sized data")
	}
}

func TestMountWrapsSyscallError(t *testing.T) {
	originalMount := mountSyscall
	t.Cleanup(func() { mountSyscall = originalMount })

	mountSyscall = func(_, _, _ string, _ uintptr, _ string) error {
		return unix.EPERM
	}

	err := (&Mount{Source: "/dev/test"}).Mount("/target")
	if !errors.Is(err, unix.EPERM) {
		t.Fatalf("Mount() error = %v, want wrapped EPERM", err)
	}
}
