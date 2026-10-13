package loop

import (
	sdkLoop "github.com/kairos-io/kairos/v4/sdk/loop"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	"github.com/kairos-io/kairos/v4/sdk/types/images"
)

// setup builds the sdk loop handler out of an agent config. PartScan stays on
// because the agent attaches whole disk images whose partitions it then works
// on through /dev/loopXpN.
func setup(cfg *sdkConfig.Config) sdkLoop.Loop {
	return sdkLoop.Loop{
		FS:       cfg.Fs,
		Syscall:  cfg.Syscall,
		Logger:   cfg.Logger,
		PartScan: true,
	}
}

// Loop will setup a /dev/loopX device linked to the image file by using syscalls directly to set it.
func Loop(img *images.Image, cfg *sdkConfig.Config) (loopDevice string, err error) {
	return setup(cfg).Attach(img.File)
}

// Unloop will clear a loop device and free the underlying image linked to it.
func Unloop(loopDevice string, cfg *sdkConfig.Config) error {
	return setup(cfg).Detach(loopDevice)
}
