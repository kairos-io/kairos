package services

import (
	"fmt"

	"github.com/kairos-io/kairos/v4/sdk/machine/service"
	loggerpkg "github.com/kairos-io/kairos/v4/sdk/types/logger"
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

# Read the environment and the arguments the provider writes at bootstrap. Last
# wins, so command_args set here replaces the default above. k3s' own openrc
# script sources /etc/rancher/k3s/k3s.env the same way.
set -o allexport
if [ -f @ENVFILE@ ]; then . @ENVFILE@; fi
set +o allexport

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

# Read the environment and the arguments the provider writes at bootstrap. Last
# wins, so command_args set here replaces the default above. k3s' own openrc
# script sources /etc/rancher/k3s/k3s.env the same way.
set -o allexport
if [ -f @ENVFILE@ ]; then . @ENVFILE@; fi
set +o allexport

: "${rc_ulimit=-n 1048576 -u unlimited}"
depend() {
	need cgroups
	need net
	use dns
	after firewall
}`

// K0s Services end here

// K0sServiceNames are the two k0s services, in the order they are installed.
var K0sServiceNames = []string{"k0scontroller", "k0sworker"}

// K0sEnvFile is the file a k0s openrc script sources for its environment and
// for the arguments the provider writes at bootstrap.
//
// k0s ships no such file of its own, unlike k3s, so this path is Kairos'. The
// scripts above reach it through service.EnvFilePlaceholder rather than
// spelling it out, so the script and whatever writes to it cannot end up
// pointing at different files (#2149).
func K0sEnvFile(unit string) string {
	return fmt.Sprintf("/etc/k0s/%s.env", unit)
}

// K0sSpec describes a k0s service without naming an init system. It is the one
// place that knows which unit body and which env file each init system needs;
// callers just say which of the two services they want.
func K0sSpec(name string) service.Spec {
	openrcUnit, systemdUnit := K0sControllerOpenrc, K0sControllerSystemd
	if name == "k0sworker" {
		openrcUnit, systemdUnit = K0sWorkerOpenrc, K0sWorkerSystemd
	}

	return service.Spec{
		Name: name,
		Init: map[service.Flavor]service.InitSpec{
			service.OpenRC:  {Unit: openrcUnit, EnvFile: K0sEnvFile(name)},
			service.Systemd: {Unit: systemdUnit},
		},
	}
}

// K0sServices installs the k0s controller and worker units.
func K0sServices(logger loggerpkg.KairosLogger) error {
	for _, name := range K0sServiceNames {
		spec := K0sSpec(name)
		// The units are written for a system that is not running yet, so there
		// is no init system to reload afterwards.
		spec.NoReload = true

		svc, err := service.New(spec)
		if err != nil {
			logger.Logger.Error().Err(err).Str("service", name).Msg("Failed to create k0s service")
			return err
		}

		if err := svc.WriteUnit(); err != nil {
			logger.Logger.Error().Err(err).Str("service", name).Msg("Failed to write k0s service unit")
			return err
		}
	}

	return nil
}
