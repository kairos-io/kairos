/*
Copyright © 2026 Kairos authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package loop

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"strings"
	"syscall"
	"testing"
	"unsafe"

	sdkFs "github.com/kairos-io/kairos/v4/sdk/types/fs"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
	"github.com/twpayne/go-vfs/v5/vfst"
	"golang.org/x/sys/unix"
)

const imagePath = "/image.img"

// ioctlCall is one recorded ioctl. arg is the third syscall argument, which
// for LOOP_SET_FD is the backing file descriptor and for LOOP_SET_STATUS64 is
// a pointer to the status struct.
type ioctlCall struct {
	request uintptr
	arg     uintptr
}

// fakeSyscall records every ioctl and answers with whatever the test asked
// for. freeDevice is the number LOOP_CTL_GET_FREE hands back.
type fakeSyscall struct {
	freeDevice uintptr
	errnos     map[uintptr]syscall.Errno
	calls      []ioctlCall
}

func (f *fakeSyscall) Syscall(trap, _, request, arg uintptr) (uintptr, uintptr, syscall.Errno) {
	if trap != syscall.SYS_IOCTL {
		return 0, 0, syscall.ENOSYS
	}

	f.calls = append(f.calls, ioctlCall{request: request, arg: arg})
	if errno, ok := f.errnos[request]; ok && errno != 0 {
		return 0, 0, errno
	}
	if request == unix.LOOP_CTL_GET_FREE {
		return f.freeDevice, 0, 0
	}

	return 0, 0, 0
}

func (f *fakeSyscall) requests() []uintptr {
	requests := make([]uintptr, 0, len(f.calls))
	for _, call := range f.calls {
		requests = append(requests, call.request)
	}

	return requests
}

// last returns the newest recorded call for a request.
func (f *fakeSyscall) last(request uintptr) (ioctlCall, bool) {
	for i := len(f.calls) - 1; i >= 0; i-- {
		if f.calls[i].request == request {
			return f.calls[i], true
		}
	}

	return ioctlCall{}, false
}

// openRecord is one recorded OpenFile, so a test can assert the mode a file
// was opened with. The kernel forces a loop device read-only when either the
// device or the backing file was opened without write access, so the modes
// are part of the contract and not an implementation detail.
type openRecord struct {
	name string
	flag int
}

// recordingFS wraps a filesystem to record every OpenFile and, optionally, to
// fail the writable open of one path with a chosen errno. That is how a
// read-only mount behaves, which no in-memory filesystem reproduces on its
// own.
type recordingFS struct {
	sdkFs.KairosFS
	opens        []openRecord
	readOnlyPath string
	readOnlyErr  error
}

func (r *recordingFS) OpenFile(name string, flag int, perm fs.FileMode) (*os.File, error) {
	r.opens = append(r.opens, openRecord{name: name, flag: flag})
	if name == r.readOnlyPath && flag&(os.O_RDWR|os.O_WRONLY) != 0 {
		return nil, &os.PathError{Op: "open", Path: name, Err: r.readOnlyErr}
	}

	return r.KairosFS.OpenFile(name, flag, perm)
}

func (r *recordingFS) flagsFor(name string) []int {
	var flags []int
	for _, open := range r.opens {
		if open.name == name {
			flags = append(flags, open.flag)
		}
	}

	return flags
}

// newLoop builds a Loop over an in-memory filesystem that already holds the
// loop control device, /dev/loop7 and the image.
func newLoop(t *testing.T) (Loop, *recordingFS, *fakeSyscall, *bytes.Buffer) {
	t.Helper()

	testFS, cleanup, err := vfst.NewTestFS(map[string]interface{}{
		loopControl:  "",
		"/dev/loop7": "",
		imagePath:    "image data",
		"/other.img": "other data",
	})
	if err != nil {
		t.Fatalf("building the test filesystem: %v", err)
	}
	t.Cleanup(cleanup)

	memLog := &bytes.Buffer{}
	log := logger.NewBufferLogger(memLog)
	log.SetLevel("debug")

	recording := &recordingFS{KairosFS: testFS}
	syscalls := &fakeSyscall{freeDevice: 7, errnos: map[uintptr]syscall.Errno{}}

	return Loop{FS: recording, Syscall: syscalls, Logger: log}, recording, syscalls, memLog
}

func TestAttachReturnsTheFreeDevice(t *testing.T) {
	l, _, syscalls, _ := newLoop(t)

	device, err := l.Attach(imagePath)
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if device != "/dev/loop7" {
		t.Errorf("device = %q, want /dev/loop7", device)
	}

	want := []uintptr{unix.LOOP_CTL_GET_FREE, unix.LOOP_SET_FD, unix.LOOP_SET_STATUS64}
	got := syscalls.requests()
	if len(got) != len(want) {
		t.Fatalf("ioctls = %#x, want %#x", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ioctls = %#x, want %#x", got, want)
		}
	}
}

// The status ioctl has to arrive after the fd is bound: LOOP_SET_STATUS64
// answers ENXIO while the device is unbound, so an inverted order would
// silently leave the backing file name and partscan unset.
func TestAttachSetsStatusOnTheBoundDevice(t *testing.T) {
	l, _, syscalls, _ := newLoop(t)

	if _, err := l.Attach(imagePath); err != nil {
		t.Fatalf("Attach: %v", err)
	}

	status, ok := syscalls.last(unix.LOOP_SET_STATUS64)
	if !ok {
		t.Fatal("LOOP_SET_STATUS64 was never issued")
	}
	if status.arg == 0 {
		t.Fatal("LOOP_SET_STATUS64 was handed a nil status struct")
	}

	info := *(*unix.LoopInfo64)(unsafe.Pointer(status.arg)) //nolint:govet // reading back the struct the ioctl was handed
	name := string(bytes.TrimRight(info.File_name[:], "\x00"))
	if name != imagePath {
		t.Errorf("lo_file_name = %q, want %q", name, imagePath)
	}
}

// The device and the backing file both have to be opened writable, or the
// kernel marks the loop device read-only for a perfectly writable image.
func TestAttachOpensDeviceAndImageWritable(t *testing.T) {
	l, testFS, _, _ := newLoop(t)

	if _, err := l.Attach(imagePath); err != nil {
		t.Fatalf("Attach: %v", err)
	}

	for _, name := range []string{"/dev/loop7", imagePath} {
		flags := testFS.flagsFor(name)
		if len(flags) != 1 {
			t.Fatalf("%s was opened %d times, want 1", name, len(flags))
		}
		if flags[0]&os.O_RDWR == 0 {
			t.Errorf("%s was opened with %#o, want O_RDWR", name, flags[0])
		}
	}
}

func TestNewLoopInfoSetsPartScanOnlyWhenAsked(t *testing.T) {
	if flags := newLoopInfo(imagePath, false).Flags; flags != 0 {
		t.Errorf("flags without partscan = %#x, want 0", flags)
	}
	if flags := newLoopInfo(imagePath, true).Flags; flags != unix.LO_FLAGS_PARTSCAN {
		t.Errorf("flags with partscan = %#x, want %#x", flags, unix.LO_FLAGS_PARTSCAN)
	}
}

// LO_FLAGS_READ_ONLY is not in LOOP_SET_STATUS_SETTABLE_FLAGS, so the kernel
// masks it straight back out. Setting it here would read as "this device is
// read-only" to anyone maintaining the file while doing nothing at all.
func TestNewLoopInfoNeverSetsReadOnly(t *testing.T) {
	for _, partScan := range []bool{false, true} {
		if flags := newLoopInfo(imagePath, partScan).Flags; flags&unix.LO_FLAGS_READ_ONLY != 0 {
			t.Errorf("flags with partScan=%v = %#x, LO_FLAGS_READ_ONLY must not be set", partScan, flags)
		}
	}
}

// The kernel copies all LO_NAME_SIZE bytes before terminating the field, so a
// path at or past the field width has to lose its tail here rather than the
// terminator.
func TestNewLoopInfoTruncatesLongPaths(t *testing.T) {
	long := "/run/initramfs/cos-state/" + strings.Repeat("a", nameSize)

	info := newLoopInfo(long, false)
	if info.File_name[nameSize-1] != 0 {
		t.Error("lo_file_name lost its NUL terminator")
	}
	name := string(bytes.TrimRight(info.File_name[:], "\x00"))
	if name != long[:nameSize-1] {
		t.Errorf("lo_file_name = %q, want %q", name, long[:nameSize-1])
	}
}

// An image on a read-only mount is what the UKI live media path hands over.
// losetup(8) attaches it read-only rather than refusing, and so does this.
func TestAttachFallsBackToReadOnly(t *testing.T) {
	for _, errno := range []error{unix.EROFS, unix.EACCES} {
		t.Run(errno.(syscall.Errno).Error(), func(t *testing.T) {
			l, testFS, syscalls, memLog := newLoop(t)
			testFS.readOnlyPath = imagePath
			testFS.readOnlyErr = errno

			device, err := l.Attach(imagePath)
			if err != nil {
				t.Fatalf("Attach: %v", err)
			}
			if device != "/dev/loop7" {
				t.Errorf("device = %q, want /dev/loop7", device)
			}

			flags := testFS.flagsFor(imagePath)
			if len(flags) != 2 {
				t.Fatalf("the image was opened %d times, want 2 (writable then read-only)", len(flags))
			}
			if flags[1]&(os.O_RDWR|os.O_WRONLY) != 0 {
				t.Errorf("the retry opened the image with %#o, want read-only", flags[1])
			}
			if _, ok := syscalls.last(unix.LOOP_SET_FD); !ok {
				t.Error("the image was never bound to the device")
			}
			if !strings.Contains(memLog.String(), "attaching read-only") {
				t.Error("the read-only fallback was not logged")
			}
		})
	}
}

// Only the two errnos that mean "you may not write this" get a second try. A
// missing image is a missing image, and retrying it read-only would replace
// the real error with a confusing one.
func TestAttachDoesNotRetryOtherOpenErrors(t *testing.T) {
	l, testFS, syscalls, _ := newLoop(t)
	testFS.readOnlyPath = imagePath
	testFS.readOnlyErr = unix.EIO

	if _, err := l.Attach(imagePath); !errors.Is(err, unix.EIO) {
		t.Fatalf("Attach error = %v, want EIO", err)
	}
	if flags := testFS.flagsFor(imagePath); len(flags) != 1 {
		t.Errorf("the image was opened %d times, want 1", len(flags))
	}
	if _, ok := syscalls.last(unix.LOOP_SET_FD); ok {
		t.Error("a device was bound despite the image not opening")
	}
}

func TestAttachErrors(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(Loop, *recordingFS, *fakeSyscall)
		wantDevice string
		wantLog    string
	}{
		{
			name: "the loop control device is missing",
			setup: func(_ Loop, testFS *recordingFS, _ *fakeSyscall) {
				if err := testFS.RemoveAll(loopControl); err != nil {
					panic(err)
				}
			},
			wantLog: "failed to open /dev/loop-control",
		},
		{
			name: "no loop device is free",
			setup: func(_ Loop, _ *recordingFS, syscalls *fakeSyscall) {
				syscalls.errnos[unix.LOOP_CTL_GET_FREE] = syscall.EBUSY
			},
			wantLog: "failed to get loop device",
		},
		{
			name: "the free loop device does not exist",
			setup: func(_ Loop, _ *recordingFS, syscalls *fakeSyscall) {
				syscalls.freeDevice = 99
			},
			wantDevice: "/dev/loop99",
			wantLog:    "failed to open loop device",
		},
		{
			name: "the image does not exist",
			setup: func(_ Loop, testFS *recordingFS, _ *fakeSyscall) {
				testFS.readOnlyPath = imagePath
				testFS.readOnlyErr = unix.ENOENT
			},
			wantDevice: "/dev/loop7",
			wantLog:    "failed to open image file",
		},
		{
			name: "binding the image fails",
			setup: func(_ Loop, _ *recordingFS, syscalls *fakeSyscall) {
				syscalls.errnos[unix.LOOP_SET_FD] = syscall.EBUSY
			},
			wantDevice: "/dev/loop7",
			wantLog:    "failed to set loop device",
		},
		{
			name: "setting the status fails",
			setup: func(_ Loop, _ *recordingFS, syscalls *fakeSyscall) {
				syscalls.errnos[unix.LOOP_SET_STATUS64] = syscall.EINVAL
			},
			wantDevice: "/dev/loop7",
			wantLog:    "failed to set loop device status",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			l, testFS, syscalls, memLog := newLoop(t)
			test.setup(l, testFS, syscalls)

			device, err := l.Attach(imagePath)
			if err == nil {
				t.Fatal("Attach succeeded, want an error")
			}
			// The device is reported alongside the error once it is
			// known, so the caller can name what it failed on.
			if device != test.wantDevice {
				t.Errorf("device = %q, want %q", device, test.wantDevice)
			}
			if !strings.Contains(memLog.String(), test.wantLog) {
				t.Errorf("log does not mention %q:\n%s", test.wantLog, memLog.String())
			}
		})
	}
}

func TestDetach(t *testing.T) {
	l, _, syscalls, _ := newLoop(t)

	if err := l.Detach("/dev/loop7"); err != nil {
		t.Fatalf("Detach: %v", err)
	}
	if _, ok := syscalls.last(unix.LOOP_CLR_FD); !ok {
		t.Error("LOOP_CLR_FD was never issued")
	}
}

func TestDetachErrors(t *testing.T) {
	tests := []struct {
		name    string
		device  string
		errno   syscall.Errno
		wantLog string
	}{
		{
			name:    "the device does not exist",
			device:  "/dev/loop99",
			wantLog: "failed to open loop device",
		},
		{
			name:    "clearing the device fails",
			device:  "/dev/loop7",
			errno:   syscall.EBUSY,
			wantLog: "failed to clear loop device",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			l, _, syscalls, memLog := newLoop(t)
			if test.errno != 0 {
				syscalls.errnos[unix.LOOP_CLR_FD] = test.errno
			}

			if err := l.Detach(test.device); err == nil {
				t.Fatal("Detach succeeded, want an error")
			}
			if !strings.Contains(memLog.String(), test.wantLog) {
				t.Errorf("log does not mention %q:\n%s", test.wantLog, memLog.String())
			}
		})
	}
}

// The zero value has to be usable, because immucore builds one with no
// filesystem and no syscall implementation to inject.
func TestZeroValueTalksToTheHost(t *testing.T) {
	l := Loop{}
	if l.fs() == nil {
		t.Error("fs() returned nil for the zero value")
	}
	if _, ok := l.syscall().(hostSyscall); !ok {
		t.Errorf("syscall() = %T, want hostSyscall", l.syscall())
	}
}
