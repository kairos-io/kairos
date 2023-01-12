package hook

import (
	config "github.com/kairos-io/kairos/pkg/config"
)

type Interface interface {
	Run(c config.Config) error
}

var All = []Interface{
	&RunStage{},    // Shells out to stages defined from the container image
	&GrubOptions{}, // Set custom GRUB options
	&BundleOption{},
	&CustomMounts{}, // add user persitent mounts
	&Kcrypt{},
	&Lifecycle{}, // Handles poweroff/reboot by config options
}

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
