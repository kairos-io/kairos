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

// WebUIDeprecationNotice is what `kairos-agent webui` prints before it
// delegates. The subcommand exists only so the live CD's kairos-webui service
// keeps working while the web UI moves into the installer, where an
// interactive boot already serves it in-process.
const WebUIDeprecationNotice = "`kairos-agent webui` is deprecated and will be removed: the web UI is served by the installer. Run the installer with --no-tui instead."

// WebUI resolves the same installer binary and asks it to serve only its web
// UI, with no terminal UI. The web installer is a frontend of the installer,
// not of the agent, so it has to come from whichever installer the image
// resolves to; an image that ships its own installer serves its own web UI.
//
// It forwards SIGTERM and SIGINT to the installer, because supervise-daemon
// signals this process and not the child holding the listen address. See
// runExternalInstallerSupervised.
//
// Deprecated: call the resolved installer with --no-tui. This subcommand is
// kept for the kairos-webui service and goes away with it.
func WebUI(source string, logger sdkLogger.KairosLogger) error {
	logger.Warnf("%s", WebUIDeprecationNotice)

	path, err := resolveInstaller()
	if err != nil {
		return err
	}

	logger.Infof("Delegating the web UI to %s", path)
	return runExternalInstallerSupervised(path, source, "--no-tui")
}
