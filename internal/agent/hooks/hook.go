package hook

import (
	config "github.com/c3os-io/c3os/pkg/config"
)

type Interface interface {
	Run(c config.Config) error
}

var All = []Interface{
	&RunStage{},    // Shells out to stages defined from the container image
	&GrubOptions{}, // Set custom GRUB options
	&Lifecycle{},   // Handles poweroff/reboot by config options
}

func Run(c config.Config, hooks ...Interface) error {
	for _, h := range hooks {
		if err := h.Run(c); err != nil {
			return err
		}
	}
	return nil
}
