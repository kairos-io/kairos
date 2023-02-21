package hook

import (
	config "github.com/kairos-io/kairos/pkg/config"
)

type Interface interface {
	Run(c config.Config) error
}

var AfterInstall = []Interface{
	&RunStage{},    // Shells out to stages defined from the container image
	&GrubOptions{}, // Set custom GRUB options
	&BundleOption{},
	&CustomMounts{},
	&Kcrypt{},
	&Lifecycle{}, // Handles poweroff/reboot by config options
}

var AfterReset []Interface

var FirstBoot = []Interface{
	&BundlePostInstall{},
	&GrubPostInstallOptions{},
}

func Run(c config.Config, hooks ...Interface) error {
	for _, h := range hooks {
		if err := h.Run(c); err != nil {
			return err
		}
	}
	return nil
}
