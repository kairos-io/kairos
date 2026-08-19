package hook

import (
	"fmt"
	"strings"

	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
	sdkLogger "github.com/kairos-io/kairos-sdk/types/logger"
	sdkSpec "github.com/kairos-io/kairos-sdk/types/spec"
	"github.com/kairos-io/kairos-sdk/utils"
)

type Interface interface {
	Run(c sdkConfig.Config, spec sdkSpec.Spec) error
}

// FinishInstall is a list of hooks that run when the install process is finished completely.
// Its mean for options that are not related to the install process itself
var FinishInstall = []Interface{
	&Lifecycle{}, // Handles poweroff/reboot by config options
}

// FinishReset is a list of hooks that run when the reset process is finished completely.
var FinishReset = []Interface{
	&CopyLogs{},  // Try to copy the reset logs to the persistent partition
	&Lifecycle{}, // Handles poweroff/reboot by config options
}

// FinishUpgrade is a list of hooks that run when the upgrade process is finished completely.
var FinishUpgrade = []Interface{
	&Lifecycle{}, // Handles poweroff/reboot by config options
}

// FirstBoot is a list of hooks that run on the first boot of the node.
var FirstBoot = []Interface{
	&BundleFirstBoot{},
	&GrubFirstBootOptions{},
}

// FinishUKIInstall is a list of hooks that run when the install process is finished completely.
// Its mean for options that are not related to the install process itself
var FinishUKIInstall = []Interface{
	&SysExtPostInstall{}, // Installs sysexts into the EFI partition
	&Lifecycle{},         // Handles poweroff/reboot by config options
}

// PostInstall is a list of hooks that run after the install process has run.
// Runs things that need to be done before we run other post install stages like
// encrypting partitions, copying the install logs or installing bundles
// Most of this options are optional so they are not run by default unless specified in the config
var PostInstall = []Interface{
	&Finish{},
}

func Run(c sdkConfig.Config, spec sdkSpec.Spec, hooks ...Interface) error {
	for _, h := range hooks {
		if err := h.Run(c, spec); err != nil {
			return err
		}
	}
	return nil
}

// lockPartitions will try to close all the partitions that are unencrypted.
func lockPartitions(log sdkLogger.KairosLogger) {
	_, _ = utils.SH("udevadm trigger --type=all || udevadm trigger")

	// Get list of active mapper devices
	dmOutput, err := utils.SH("dmsetup ls --target crypt")
	if err != nil {
		log.Debugf("could not list dm devices: %v", err)
		return
	}

	// Parse dmsetup output (format: "vda2  (252:1)")
	lines := strings.Split(strings.TrimSpace(dmOutput), "\n")
	for _, line := range lines {
		if line == "" || strings.Contains(line, "No devices found") {
			continue
		}

		// Extract mapper name (first field before whitespace)
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		mapperName := fields[0]

		log.Debugf("Closing encrypted device: %s", mapperName)
		out, err := utils.SH(fmt.Sprintf("cryptsetup close %s", mapperName))
		// There is a known error with cryptsetup that it can't close the device because of a semaphore
		// doesnt seem to affect anything as the device is closed as expected so we ignore it if it matches the
		// output of the error
		if err != nil && !strings.Contains(out, "incorrect semaphore state") {
			log.Debugf("could not close %s: %s", mapperName, out)
		}
	}
}
