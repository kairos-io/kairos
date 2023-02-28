package hook

import (
	config "github.com/kairos-io/kairos/pkg/config"
	schema "github.com/kairos-io/kairos/pkg/config/schemas"
)

type Interface interface {
	Run(c config.Config) error
	KRun(c schema.KConfig) error
}

var AfterInstall = []Interface{
	&RunStage{},    // Shells out to stages defined from the container image
	&GrubOptions{}, // Set custom GRUB options
	&BundleOption{},
	&CustomMounts{},
	&Kcrypt{},
	&Lifecycle{}, // Handles poweroff/reboot by config options
}

var AfterReset = []Interface{
	&Kcrypt{},
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

// KRun is a temporary function that does the same as Run. It will be removed as soon as the transition from config.Config to schema.KConfig is finished.
func KRun(kc schema.KConfig, hooks ...Interface) error {
	for _, h := range hooks {
		if err := h.KRun(kc); err != nil {
			return err
		}
	}
	return nil
}
