package agent

import (
	"fmt"
	"strings"

	"github.com/kairos-io/kairos/v4/agent/pkg/config"
	"github.com/kairos-io/kairos/v4/sdk/collector"
	"github.com/kairos-io/kairos/v4/sdk/constants"
	"github.com/kairos-io/kairos/v4/sdk/installer"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
	"github.com/kairos-io/kairos/v4/sdk/utils"
)

// InteractiveInstall resolves an external installer binary and delegates the
// interactive installation UX to it. There is no in-process fallback.
// - spawnShell: if true, spawn a shell after the installer exits.
// - source: installation source, forwarded to the installer.
// - dir: config directories to scan for an unattended install.
func InteractiveInstall(spawnShell bool, source string, logger sdkLogger.KairosLogger, dir ...string) error {
	// install.auto is an instruction to install without asking anything, so it
	// wins over the UX. This entrypoint is what an install-mode-interactive
	// boot runs, and an unattended boot must install rather than stop at a
	// prompt nobody is there to answer.
	if cc, err := scanForAutoInstall(source, dir...); err != nil {
		logger.Debugf("No unattended config, continuing to the interactive installer: %s", err.Error())
	} else if autoInstallRequested(cc) {
		logger.Infof("install.auto is set, installing without the interactive installer")
		return runAutoInstall(cc)
	}

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

// scanForAutoInstall reads the config the same way the install-mode entrypoint
// does, so the two agree on whether an unattended install was asked for. The
// wait for the datasource is what keeps a config that is still being written
// from being read as absent.
func scanForAutoInstall(source string, dir ...string) (*sdkConfig.Config, error) {
	ensureDataSourceReady()

	return config.Scan(
		collector.Directories(dir...),
		collector.Readers(strings.NewReader(generateInstallConfForCLIArgs(source, false))),
		collector.MergeBootLine,
	)
}
