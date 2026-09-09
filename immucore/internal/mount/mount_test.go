package mount

import (
	"errors"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// call records one invocation of the mount syscall.
type call struct {
	source, target, fstype, data string
	flags                        uintptr
}

// record swaps the syscall for a recorder and returns the calls it collected.
func record(t *testing.T, err error, f func()) *[]call {
	t.Helper()

	var calls []call
	original := mountSyscall
	mountSyscall = func(source, target, fstype string, flags uintptr, data string) error {
		calls = append(calls, call{source: source, target: target, fstype: fstype, flags: flags, data: data})
		return err
	}
	t.Cleanup(func() { mountSyscall = original })

	f()
	return &calls
}

func TestParseOptionsSplitsFlagsFromData(t *testing.T) {
	// The option set immucore actually passes: block device modes, a bind, a
	// tmpfs size and the three overlay dirs.
	flags, data := ParseOptions([]string{"ro", "suid", "dev", "exec", "async", "size=50%", "lowerdir=/a"})

	if flags&unix.MS_RDONLY == 0 {
		t.Error("ro did not set MS_RDONLY")
	}
	for name, flag := range map[string]uintptr{"suid": unix.MS_NOSUID, "dev": unix.MS_NODEV, "exec": unix.MS_NOEXEC, "async": unix.MS_SYNCHRONOUS} {
		if flags&flag != 0 {
			t.Errorf("%s left its negative flag set", name)
		}
	}
	if len(data) != 2 || data[0] != "size=50%" || data[1] != "lowerdir=/a" {
		t.Errorf("unknown options should become data, got %v", data)
	}
}

func TestParseOptionsClearingOptionsUndoSetters(t *testing.T) {
	// Order matters and the last one wins, as mount(8) behaves.
	if flags, _ := ParseOptions([]string{"ro", "rw"}); flags&unix.MS_RDONLY != 0 {
		t.Error("rw after ro left the mount read-only")
	}
	if flags, _ := ParseOptions([]string{"rw", "ro"}); flags&unix.MS_RDONLY == 0 {
		t.Error("ro after rw did not make the mount read-only")
	}
}

func TestParseOptionsDefaultsIsNotData(t *testing.T) {
	// containerd's table gave "defaults" a zero flag but then tested the flag
	// against zero, so "defaults" fell through to the data string and any
	// filesystem that validates its options rejected the mount.
	flags, data := ParseOptions([]string{"defaults"})
	if flags != 0 {
		t.Errorf("defaults should set no flag, got %#x", flags)
	}
	if len(data) != 0 {
		t.Errorf("defaults should not reach the data string, got %v", data)
	}
}

func TestMountPassesSourceTypeAndData(t *testing.T) {
	m := Mount{Type: "tmpfs", Source: "tmpfs", Options: []string{"rw", "size=50%"}}

	calls := record(t, nil, func() {
		if err := m.Mount("/tmp"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	if len(*calls) != 1 {
		t.Fatalf("expected one syscall, got %d", len(*calls))
	}
	got := (*calls)[0]
	if got.source != "tmpfs" || got.target != "/tmp" || got.fstype != "tmpfs" || got.data != "size=50%" {
		t.Errorf("unexpected syscall arguments: %+v", got)
	}
}

func TestMountRemountsAReadOnlyBind(t *testing.T) {
	// mount(2) ignores MS_RDONLY when MS_BIND is set, so a "bind,ro" mount comes
	// up writable unless a second MS_REMOUNT call applies it.
	m := Mount{Type: "overlay", Source: "/run/state/etc.bind", Options: []string{"bind", "ro"}}

	calls := record(t, nil, func() {
		if err := m.Mount("/sysroot/etc"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	if len(*calls) != 2 {
		t.Fatalf("a read-only bind needs two syscalls, got %d: %+v", len(*calls), *calls)
	}
	// The initial call has to carry what ParseOptions computed. Nothing else in
	// the suite covers that: the remount below builds on the same local, so
	// passing 0 for flags on the first syscall would leave every other test
	// green while mounting /sysroot writable and dropping suid/dev/exec/async
	// from /oem.
	const readOnlyBind = unix.MS_BIND | unix.MS_RDONLY
	if initial := (*calls)[0]; initial.flags&readOnlyBind != readOnlyBind {
		t.Errorf("the first call dropped the parsed flags: %#x", initial.flags)
	}
	remount := (*calls)[1]
	if remount.flags&unix.MS_REMOUNT == 0 {
		t.Error("the second call is not a remount")
	}
	if remount.flags&unix.MS_RDONLY == 0 {
		t.Error("the remount does not carry MS_RDONLY")
	}
	if remount.target != "/sysroot/etc" {
		t.Errorf("the remount targets %q", remount.target)
	}
}

func TestMountDoesNotRemountAWritableBind(t *testing.T) {
	m := Mount{Type: "overlay", Source: "/run/state/etc.bind", Options: []string{"bind"}}

	calls := record(t, nil, func() {
		if err := m.Mount("/sysroot/etc"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	if len(*calls) != 1 {
		t.Fatalf("a writable bind needs one syscall, got %d: %+v", len(*calls), *calls)
	}
}

func TestMountWrapsTheSyscallError(t *testing.T) {
	m := Mount{Type: "ext4", Source: "/dev/sda1", Options: []string{"rw"}}

	var err error
	record(t, unix.ENOENT, func() { err = m.Mount("/sysroot") })

	if !errors.Is(err, unix.ENOENT) {
		t.Fatalf("the syscall error should be wrapped, got %v", err)
	}
	// The retry loop in pkg/op logs this, so both paths have to be in it.
	for _, want := range []string{"/dev/sda1", "/sysroot"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestMountRejectsDataLongerThanAPage(t *testing.T) {
	// Derived, not hardcoded: Mount compares against os.Getpagesize(), which
	// the runtime reads from auxv AT_PAGESZ rather than fixing per GOARCH. A
	// literal "longer than 4K" payload silently stops being oversized on a
	// 64K-page arm64 kernel, and the test would fail there instead of passing.
	long := make([]byte, unix.Getpagesize())
	for i := range long {
		long[i] = 'a'
	}
	m := Mount{Type: "overlay", Source: "overlay", Options: []string{"lowerdir=" + string(long)}}

	calls := record(t, nil, func() {
		if err := m.Mount("/sysroot"); err == nil {
			t.Fatal("expected an error for oversized mount data")
		}
	})

	if len(*calls) != 0 {
		t.Error("the syscall should not run with oversized data")
	}
}

func TestAllMountsEveryEntryAndStopsAtTheFirstError(t *testing.T) {
	mounts := []Mount{
		{Type: "tmpfs", Source: "tmpfs", Options: []string{"rw"}},
		{Type: "tmpfs", Source: "tmpfs", Options: []string{"ro"}},
	}

	calls := record(t, nil, func() {
		if err := All(mounts, "/tmp"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	if len(*calls) != 2 {
		t.Errorf("expected both mounts to run, got %d", len(*calls))
	}

	failing := record(t, unix.EPERM, func() {
		if err := All(mounts, "/tmp"); err == nil {
			t.Fatal("expected the first failure to be returned")
		}
	})
	if len(*failing) != 1 {
		t.Errorf("expected to stop after the first failure, got %d calls", len(*failing))
	}
}
