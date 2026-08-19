package loop

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"

	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
	"github.com/kairos-io/kairos-sdk/types/images"
	"golang.org/x/sys/unix"
)

// syscalls will return an errno type (which implements error) for all calls,
// including success (errno 0) so we need to check its value to know if its an actual error or not
func errnoIsErr(err error) error {
	if err != nil && err.(syscall.Errno) != 0 {
		return err
	}

	return nil
}

// Loop will setup a /dev/loopX device linked to the image file by using syscalls directly to set it
func Loop(img *images.Image, cfg *sdkConfig.Config) (loopDevice string, err error) {
	log := cfg.Logger
	log.Debugf("Opening loop control device")
	fd, err := cfg.Fs.OpenFile("/dev/loop-control", os.O_RDONLY, 0o644)
	if err != nil {
		log.Error("failed to open /dev/loop-control")
		return loopDevice, err
	}

	defer fd.Close()
	log.Debugf("Getting free loop device")
	loopInt, _, err := cfg.Syscall.Syscall(syscall.SYS_IOCTL, fd.Fd(), unix.LOOP_CTL_GET_FREE, 0)
	if errnoIsErr(err) != nil {
		log.Error("failed to get loop device")
		return loopDevice, err
	}

	loopDevice = fmt.Sprintf("/dev/loop%d", loopInt)
	log.Logger.Debug().Str("device", loopDevice).Msg("Opening loop device")
	loopFile, err := cfg.Fs.OpenFile(loopDevice, os.O_RDWR, 0)
	if err != nil {
		log.Error("failed to open loop device")
		return loopDevice, err
	}
	log.Logger.Debug().Str("image", img.File).Msg("Opening img file")
	imageFile, err := cfg.Fs.OpenFile(img.File, os.O_RDWR, os.ModePerm)
	if err != nil {
		log.Error("failed to open image file")
		return loopDevice, err
	}
	defer loopFile.Close()
	defer imageFile.Close()

	log.Debugf("Setting loop device")
	_, _, err = cfg.Syscall.Syscall(
		syscall.SYS_IOCTL,
		loopFile.Fd(),
		unix.LOOP_SET_FD,
		imageFile.Fd(),
	)
	if errnoIsErr(err) != nil {
		log.Error("failed to set loop device")
		return loopDevice, err
	}

	// Force kernel to scan partition table on loop device
	status := &unix.LoopInfo64{
		Flags: unix.LO_FLAGS_PARTSCAN,
	}
	// Dont set read only flag
	status.Flags &= ^uint32(unix.LO_FLAGS_READ_ONLY)

	log.Debugf("Setting loop flags")
	_, _, err = cfg.Syscall.Syscall(
		syscall.SYS_IOCTL,
		loopFile.Fd(),
		unix.LOOP_SET_STATUS64,
		uintptr(unsafe.Pointer(status)),
	)

	if errnoIsErr(err) != nil {
		log.Error("failed to set loop device status")
		return loopDevice, err
	}

	return loopDevice, nil
}

// Unloop will clear a loop device and free the underlying image linked to it
func Unloop(loopDevice string, cfg *sdkConfig.Config) error {
	log := cfg.Logger
	log.Logger.Debug().Str("device", loopDevice).Msg("Opening loop device")
	fd, err := cfg.Fs.OpenFile(loopDevice, os.O_RDONLY, 0o644)
	if err != nil {
		log.Error("failed to set open loop device")
		return err
	}
	defer fd.Close()
	log.Debugf("Clearing loop device")
	_, _, err = cfg.Syscall.Syscall(syscall.SYS_IOCTL, fd.Fd(), unix.LOOP_CLR_FD, 0)

	if errnoIsErr(err) != nil {
		log.Error("failed to set loop device status")
		return err
	}

	return nil
}
