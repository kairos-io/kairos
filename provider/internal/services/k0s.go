package services

import (
	"fmt"

	"github.com/kairos-io/kairos/v4/sdk/machine"
	"github.com/kairos-io/kairos/v4/sdk/machine/openrc"
	"github.com/kairos-io/kairos/v4/sdk/machine/systemd"
	loggerpkg "github.com/kairos-io/kairos/v4/sdk/types/logger"
	"github.com/kairos-io/kairos/v4/sdk/utils"
)

// K0s Services start here

// K0sControllerUnit and K0sWorkerUnit are the service names, and also the base
// name of the environment file machine.K0sEnvUnit builds for each of them.
const (
	K0sControllerUnit = "k0scontroller"
	K0sWorkerUnit     = "k0sworker"
)

// The environment file carries what the user put under k0s.env and
// k0s-worker.env, so the unit has to read it. The leading dash keeps the unit
// startable before the file exists, which is the case until the provider
// bootstraps the node.
const k0sSystemd = `[Unit]
Description=k0s - Zero Friction Kubernetes
Documentation=https://docs.k0sproject.io
ConditionFileIsExecutable=/usr/bin/k0s

After=network-online.target
Wants=network-online.target

[Service]
StartLimitInterval=5
StartLimitBurst=10
EnvironmentFile=-%[2]s
ExecStart=/usr/bin/k0s %[1]s

RestartSec=10
Delegate=yes
KillMode=process
LimitCORE=infinity
TasksMax=infinity
TimeoutStartSec=0
LimitNOFILE=999999
Restart=always

[Install]
WantedBy=multi-user.target`

// OpenRC has no EnvironmentFile, so the script sources the same file itself.
// allexport is what makes the variables reach the supervised process, since
// the file holds plain assignments with no export in front of them.
const k0sOpenRC = `#!/sbin/openrc-run
supervisor=supervise-daemon
description="k0s - Zero Friction Kubernetes"
command=/usr/bin/k0s
command_args="'%[1]s' "
name=$(basename $(readlink -f $command))
supervise_daemon_args="--stdout /var/log/${name}.log --stderr /var/log/${name}.err"
set -o allexport
if [ -f %[2]s ]; then source %[2]s; fi
set +o allexport

: "${rc_ulimit=-n 1048576 -u unlimited}"
depend() {
	need cgroups
	need net
	use dns
	after firewall
}`

// K0sSystemdUnit builds the systemd unit for the given k0s command, reading
// its environment from envFile.
func K0sSystemdUnit(command, envFile string) string {
	return fmt.Sprintf(k0sSystemd, command, envFile)
}

// K0sOpenRCUnit builds the OpenRC service script for the given k0s command,
// reading its environment from envFile.
func K0sOpenRCUnit(command, envFile string) string {
	return fmt.Sprintf(k0sOpenRC, command, envFile)
}

// K0s Services end here

// K0sServices creates the k0s controller and worker services for openrc or systemd based systems.
func K0sServices(logger loggerpkg.KairosLogger) error {
	if utils.IsOpenRCBased() {
		controller, err := openrc.NewService(
			openrc.WithName(K0sControllerUnit),
			openrc.WithUnitContent(K0sOpenRCUnit("controller", machine.K0sEnvUnit(K0sControllerUnit))),
		)
		if err != nil {
			logger.Logger.Error().Err(err).Str("init", "openrc").Msg("Failed to create k0s controller service")
			return err
		}
		if err = controller.WriteUnit(); err != nil {
			logger.Logger.Error().Err(err).Str("init", "openrc").Msg("Failed to write k0s controller service unit")
			return err
		}
		worker, err := openrc.NewService(
			openrc.WithName(K0sWorkerUnit),
			openrc.WithUnitContent(K0sOpenRCUnit("worker", machine.K0sEnvUnit(K0sWorkerUnit))),
		)

		if err != nil {
			logger.Logger.Error().Err(err).Str("init", "openrc").Msg("Failed to create k0s worker service")
			return err
		}
		if err = worker.WriteUnit(); err != nil {
			logger.Logger.Error().Err(err).Str("init", "openrc").Msg("Failed to write k0s worker service unit")
			return err
		}

	} else {
		controller, err := systemd.NewService(
			systemd.WithName(K0sControllerUnit),
			systemd.WithUnitContent(K0sSystemdUnit("controller", machine.K0sEnvUnit(K0sControllerUnit))),
			systemd.WithReload(false), // we are not in a running system, so we cant reload
		)
		if err != nil {
			logger.Logger.Error().Err(err).Str("init", "systemd").Msg("Failed to create k0s controller service")
			return err
		}
		if err = controller.WriteUnit(); err != nil {
			logger.Logger.Error().Err(err).Str("init", "systemd").Msg("Failed to write k0s controller service unit")
			return err
		}
		worker, err := systemd.NewService(
			systemd.WithName(K0sWorkerUnit),
			systemd.WithUnitContent(K0sSystemdUnit("worker", machine.K0sEnvUnit(K0sWorkerUnit))),
			systemd.WithReload(false), // we are not in a running system, so we cant reload
		)
		if err != nil {
			logger.Logger.Error().Err(err).Str("init", "systemd").Msg("Failed to create k0s worker service")
			return err
		}
		if err = worker.WriteUnit(); err != nil {
			logger.Logger.Error().Err(err).Str("init", "systemd").Msg("Failed to write k0s worker service unit")
			return err
		}
	}

	return nil
}
