package agent

import (
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
	"github.com/kairos-io/kairos/v4/sdk/utils"
)

// InteractiveInstall resolves an external installer binary and delegates the
// interactive installation UX to it. There is no in-process fallback.
// - spawnShell: if true, spawn a shell after the installer exits.
// - source: installation source, forwarded to the installer.
func InteractiveInstall(spawnShell bool, source string, logger sdkLogger.KairosLogger) error {
	path, err := resolveInstaller()
	if err != nil {
		return err
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

// WebUI resolves the same installer binary and asks it to serve only its web
// UI, with no terminal UI. The web installer is a frontend of the installer,
// not of the agent, so it has to come from whichever installer the image
// resolves to; an image that ships its own installer serves its own web UI.
func WebUI(source string, logger sdkLogger.KairosLogger) error {
	path, err := resolveInstaller()
	if err != nil {
		return err
	}

	logger.Infof("Delegating the web UI to %s", path)
	return runExternalInstaller(path, source, "--no-tui")
}
