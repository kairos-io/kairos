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

// Package loop attaches image files to loop devices through the loop ioctls,
// so a caller does not need the losetup(8) binary. immucore runs in the
// initramfs, where every binary has to be put there on purpose, and the agent
// already drove these same ioctls from a copy of this code.
//
// What losetup(8) does that this package leaves out:
//
//   - It reads /sys and /proc to list, filter and match devices that are
//     already attached (-a, -j, -l). Attach and Detach are the only
//     operations either caller needs.
//   - It can set an offset, a size limit, a block size, direct IO and
//     autoclear. No caller uses them.
package loop

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"syscall"
	"unsafe"

	sdkFs "github.com/kairos-io/kairos/v4/sdk/types/fs"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
	"github.com/twpayne/go-vfs/v5"
	"golang.org/x/sys/unix"
)

// loopControl hands out free loop devices. Opening it does not attach
// anything, it only answers LOOP_CTL_GET_FREE.
const loopControl = "/dev/loop-control"

// nameSize is the kernel's LO_NAME_SIZE, the width of lo_file_name in
// struct loop_info64. The last byte is kept for the NUL terminator.
const nameSize = unix.LO_NAME_SIZE

// bindAttempts bounds the LOOP_SET_FD retry below. LOOP_CTL_GET_FREE reports a
// device that was free a moment ago, and on a booted system another process
// can claim it before LOOP_SET_FD lands. losetup(8) asks for another device
// rather than failing the caller, and so does this, with a ceiling so a box
// that genuinely has nothing free fails instead of spinning.
const bindAttempts = 16

// ioctler is the single method this package needs out of a syscall
// implementation. sdk/types/syscall.Interface satisfies it, so a caller that
// already carries one can pass it straight in, mock included.
type ioctler interface {
	Syscall(trap, a1, a2, a3 uintptr) (r1, r2 uintptr, err syscall.Errno)
}

// hostSyscall issues the ioctls against the running kernel. It is the default
// for callers that have no mockable implementation of their own to inject.
type hostSyscall struct{}

func (hostSyscall) Syscall(trap, a1, a2, a3 uintptr) (uintptr, uintptr, syscall.Errno) {
	return syscall.Syscall(trap, a1, a2, a3)
}

// Loop attaches image files to loop devices. The zero value works and talks
// to the host: fill FS and Syscall only to redirect it, as the agent does to
// test against a fake filesystem and fake ioctls.
type Loop struct {
	// FS is where the loop devices and the images are looked up. A nil FS
	// means the running system.
	FS sdkFs.KairosFS
	// Syscall issues the ioctls. A nil Syscall means the running kernel.
	Syscall ioctler
	// Logger records each step. Attach and Detach log the failing step before
	// returning, so a caller that only reports the error still leaves a trail.
	Logger logger.KairosLogger
	// PartScan asks the kernel to read a partition table off the attached
	// image and publish /dev/loopXpN for each partition it finds. A
	// filesystem image has no partition table and does not need it.
	PartScan bool
}

// Attach binds imagePath to a free loop device and returns the device path.
//
// The device comes out read-write for a writable image and read-only for one
// that is not. That is not a flag this code sets: the kernel derives it from
// the mode the backing file was opened with, and Attach falls back to opening
// read-only, the same as losetup(8). An image on a read-only mount therefore
// attaches read-only instead of failing to attach. Both callers rely on that:
// the UKI live media keeps its EFI boot image on read-only media, and on an
// ordinary immutable boot immucore mounts cos-state ro, so the state image
// takes the same fallback every time.
//
// The device path is returned even alongside an error once it is known, so a
// caller can name the device it failed on.
func (l Loop) Attach(imagePath string) (string, error) {
	l.Logger.Logger.Debug().Str("image", imagePath).Msg("Opening img file")
	imageFile, readOnly, err := l.openImage(imagePath)
	if err != nil {
		l.Logger.Error("failed to open image file")
		return "", err
	}
	defer imageFile.Close()

	device, loopFile, err := l.bind(imageFile)
	if err != nil {
		return device, err
	}
	defer loopFile.Close()

	info := newLoopInfo(imagePath, l.PartScan)
	// The uintptr below is an argument to an interface method, not to
	// syscall.Syscall itself, so rule 4 in the unsafe docs does not cover it
	// and the compiler is free to leave info on the stack. Handing &info to
	// Pin moves it to the heap, which the collector does not move, so the
	// address stays valid across the frames between here and the syscall.
	var pinner runtime.Pinner
	pinner.Pin(&info)
	defer pinner.Unpin()

	l.Logger.Debugf("Setting loop flags")
	if _, _, errno := l.syscall().Syscall(
		syscall.SYS_IOCTL,
		loopFile.Fd(),
		unix.LOOP_SET_STATUS64,
		uintptr(unsafe.Pointer(&info)),
	); errno != 0 {
		l.Logger.Error("failed to set loop device status")
		// losetup(8) releases the device on this path. Left bound, it holds
		// the image open for the life of the process, and the caller's next
		// attempt at the same image finds it busy. The clear failing is
		// logged and not returned, so the caller sees the status errno.
		if _, _, clrErrno := l.syscall().Syscall(syscall.SYS_IOCTL, loopFile.Fd(), unix.LOOP_CLR_FD, 0); clrErrno != 0 {
			l.Logger.Logger.Debug().Str("device", device).Err(clrErrno).Msg("Could not release the device after the status ioctl failed")
		}

		return device, errno
	}

	l.Logger.Logger.Debug().Str("device", device).Str("image", imagePath).Bool("readOnly", readOnly).Msg("Attached loop device")
	return device, nil
}

// bind claims a free loop device and binds imageFile to it, returning the
// device path and the open device. A device claimed by another process between
// LOOP_CTL_GET_FREE and LOOP_SET_FD answers EBUSY, and the whole cycle is
// retried on that, up to bindAttempts.
//
// The device path comes back alongside an error once it is known, so a caller
// can name the device it failed on.
func (l Loop) bind(imageFile *os.File) (string, *os.File, error) {
	l.Logger.Debugf("Opening loop control device")
	control, err := l.fs().OpenFile(loopControl, os.O_RDONLY, 0o644)
	if err != nil {
		l.Logger.Error("failed to open /dev/loop-control")
		return "", nil, err
	}
	defer control.Close()

	var device string
	for attempt := 1; ; attempt++ {
		l.Logger.Debugf("Getting free loop device")
		free, _, errno := l.syscall().Syscall(syscall.SYS_IOCTL, control.Fd(), unix.LOOP_CTL_GET_FREE, 0)
		if errno != 0 {
			l.Logger.Error("failed to get loop device")
			return device, nil, errno
		}

		device = fmt.Sprintf("/dev/loop%d", free)
		l.Logger.Logger.Debug().Str("device", device).Msg("Opening loop device")
		loopFile, err := l.fs().OpenFile(device, os.O_RDWR, 0)
		if err != nil {
			l.Logger.Error("failed to open loop device")
			return device, nil, err
		}

		l.Logger.Debugf("Setting loop device")
		_, _, errno = l.syscall().Syscall(syscall.SYS_IOCTL, loopFile.Fd(), unix.LOOP_SET_FD, imageFile.Fd())
		if errno == 0 {
			return device, loopFile, nil
		}
		loopFile.Close()

		if !errors.Is(errno, unix.EBUSY) || attempt == bindAttempts {
			l.Logger.Error("failed to set loop device")
			return device, nil, errno
		}

		l.Logger.Logger.Debug().Str("device", device).Int("attempt", attempt).Msg("Loop device was claimed first, asking for another")
	}
}

// Detach clears the loop device and releases the image behind it.
func (l Loop) Detach(device string) error {
	l.Logger.Logger.Debug().Str("device", device).Msg("Opening loop device")
	fd, err := l.fs().OpenFile(device, os.O_RDONLY, 0o644)
	if err != nil {
		l.Logger.Error("failed to open loop device")
		return err
	}
	defer fd.Close()

	l.Logger.Debugf("Clearing loop device")
	if _, _, errno := l.syscall().Syscall(syscall.SYS_IOCTL, fd.Fd(), unix.LOOP_CLR_FD, 0); errno != 0 {
		l.Logger.Error("failed to clear loop device")
		return errno
	}

	return nil
}

// newLoopInfo fills the status struct handed to LOOP_SET_STATUS64.
//
// The backing file name is not what makes the device work, it is what lets
// "losetup -a" and "losetup -j" report the image afterwards. The kernel
// copies it out of this struct (loop_set_status_from_info), so leaving it
// empty is what makes a device show up with no image against it.
//
// LO_FLAGS_READ_ONLY is deliberately absent. The kernel masks the incoming
// flags down to LOOP_SET_STATUS_SETTABLE_FLAGS, which is autoclear and
// partscan only, and it marks the device read-only from the mode the backing
// file was opened with instead.
func newLoopInfo(imagePath string, partScan bool) unix.LoopInfo64 {
	info := unix.LoopInfo64{}
	// The kernel NUL-terminates lo_file_name itself, but only after copying
	// the full LO_NAME_SIZE bytes, so a path that fills the field has to be
	// truncated here to keep the terminator.
	copy(info.File_name[:nameSize-1], imagePath)
	if partScan {
		info.Flags |= unix.LO_FLAGS_PARTSCAN
	}

	return info
}

// openImage opens the backing file read-write, and settles for read-only when
// the file or the mount under it will not allow writing. losetup(8) makes the
// same fallback. Without it an image on a read-only mount could not be
// attached at all: that is where the UKI live media keeps its EFI boot image,
// and it is also every ordinary immucore boot, which mounts cos-state ro and
// so reaches the state image through this same fallback.
//
// LO_FLAGS_READ_ONLY is deliberately not set on the way out: the kernel
// ignores it in LOOP_SET_STATUS64 (only autoclear and partscan are settable
// there) and marks the device read-only from the open mode instead.
func (l Loop) openImage(path string) (*os.File, bool, error) {
	imageFile, err := l.fs().OpenFile(path, os.O_RDWR, 0)
	if err == nil {
		return imageFile, false, nil
	}
	if !errors.Is(err, unix.EROFS) && !errors.Is(err, unix.EACCES) {
		return nil, false, err
	}

	// Warn, not Debug: immucore runs at info unless asked otherwise, and
	// this fires on every immutable boot. A device that turns out read-only
	// downstream should have a reason in the boot log.
	l.Logger.Logger.Warn().Str("image", path).AnErr("writable", err).Msg("Image is not writable, attaching read-only")
	imageFile, err = l.fs().OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return nil, false, err
	}

	return imageFile, true, nil
}

func (l Loop) fs() sdkFs.KairosFS {
	if l.FS == nil {
		return vfs.OSFS
	}

	return l.FS
}

func (l Loop) syscall() ioctler {
	if l.Syscall == nil {
		return hostSyscall{}
	}

	return l.Syscall
}
