package agent

import (
	"fmt"

	"github.com/kairos-io/kairos-sdk/constants"
	"github.com/kairos-io/kairos-sdk/installer"
	sdkLogger "github.com/kairos-io/kairos-sdk/types/logger"
	"github.com/kairos-io/kairos-sdk/utils"
)

// InteractiveInstall resolves an external installer binary and delegates the
// interactive installation UX to it. There is no in-process fallback.
// - spawnShell: if true, spawn a shell after the installer exits.
// - source: installation source, forwarded to the installer.
func InteractiveInstall(spawnShell bool, source string, logger sdkLogger.KairosLogger) error {
	path := installer.Resolve("/")
	if path == "" {
		return fmt.Errorf("no interactive installer found (looked for %s, %s; or set %s)",
			constants.InstallerOverridePath, constants.InstallerDefaultPath, constants.InstallerEnvVar)
	}

	logger.Infof("Delegating interactive installation to %s", path)
	if err := runExternalInstaller(path, source); err != nil {
		return err
	}
	if spawnShell {
		return utils.Shell().Run()
	}
	return nil
}
