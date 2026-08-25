package services

import (
	"github.com/kairos-io/kairos/v4/sdk/machine/openrc"
	"github.com/kairos-io/kairos/v4/sdk/machine/systemd"
	loggerpkg "github.com/kairos-io/kairos/v4/sdk/types/logger"
	"github.com/kairos-io/kairos/v4/sdk/utils"
)

// K0s Services start here

const K0sControllerSystemd = `[Unit]
Description=k0s - Zero Friction Kubernetes
Documentation=https://docs.k0sproject.io
ConditionFileIsExecutable=/usr/bin/k0s

After=network-online.target
Wants=network-online.target

[Service]
StartLimitInterval=5
StartLimitBurst=10
ExecStart=/usr/bin/k0s controller

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

const K0sWorkerSystemd = `[Unit]
Description=k0s - Zero Friction Kubernetes
Documentation=https://docs.k0sproject.io
ConditionFileIsExecutable=/usr/bin/k0s

After=network-online.target
Wants=network-online.target

[Service]
StartLimitInterval=5
StartLimitBurst=10
ExecStart=/usr/bin/k0s worker

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

const K0sControllerOpenrc = `#!/sbin/openrc-run
supervisor=supervise-daemon
description="k0s - Zero Friction Kubernetes"
command=/usr/bin/k0s
command_args="'controller' "
name=$(basename $(readlink -f $command))
supervise_daemon_args="--stdout /var/log/${name}.log --stderr /var/log/${name}.err"

: "${rc_ulimit=-n 1048576 -u unlimited}"
depend() {
	need cgroups
	need net
	use dns
	after firewall
}`

const K0sWorkerOpenrc = `#!/sbin/openrc-run
supervisor=supervise-daemon
description="k0s - Zero Friction Kubernetes"
command=/usr/bin/k0s
command_args="'worker' "
name=$(basename $(readlink -f $command))
supervise_daemon_args="--stdout /var/log/${name}.log --stderr /var/log/${name}.err"

: "${rc_ulimit=-n 1048576 -u unlimited}"
depend() {
	need cgroups
	need net
	use dns
	after firewall
}`

// K0s Services end here

// K0sServices creates the k0s controller and worker services for openrc or systemd based systems.
func K0sServices(logger loggerpkg.KairosLogger) error {
	if utils.IsOpenRCBased() {
		controller, err := openrc.NewService(
			openrc.WithName("k0scontroller"),
			openrc.WithUnitContent(K0sControllerOpenrc),
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
			openrc.WithName("k0sworker"),
			openrc.WithUnitContent(K0sWorkerOpenrc),
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
			systemd.WithName("k0scontroller"),
			systemd.WithUnitContent(K0sControllerSystemd),
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
			systemd.WithName("k0sworker"),
			systemd.WithUnitContent(K0sWorkerSystemd),
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
