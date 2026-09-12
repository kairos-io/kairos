package services

import (
	"github.com/kairos-io/kairos/v4/sdk/machine"
	"github.com/kairos-io/kairos/v4/sdk/machine/service"
)

const edgevpnOpenRC string = `#!/sbin/openrc-run

depend() {
	after net
	provide edgevpn
}

supervisor=supervise-daemon
name="edgevpn"
command="edgevpn"
supervise_daemon_args="--stdout /var/log/edgevpn.log --stderr /var/log/edgevpn.log"
pidfile="/run/edgevpn.pid"
respawn_delay=5
set -o allexport
if [ -f /etc/environment ]; then source /etc/environment; fi
if [ -f /etc/systemd/system.conf.d/edgevpn-kairos.env ]; then source /etc/systemd/system.conf.d/edgevpn-kairos.env; fi
set +o allexport`

const edgevpnAPIOpenRC string = `#!/sbin/openrc-run

depend() {
	after net
	provide edgevpn
}

supervisor=supervise-daemon
name="edgevpn"
command="edgevpn"
command_args="api --enable-healthchecks"
supervise_daemon_args="--stdout /var/log/edgevpn.log --stderr /var/log/edgevpn.log"
pidfile="/run/edgevpn.pid"
respawn_delay=5
set -o allexport
if [ -f /etc/environment ]; then source /etc/environment; fi
if [ -f /etc/systemd/system.conf.d/edgevpn-kairos.env ]; then source /etc/systemd/system.conf.d/edgevpn-kairos.env; fi
set +o allexport`

const edgevpnAPISystemd string = `[Unit]
Description=P2P API Daemon
After=network.target
[Service]
EnvironmentFile=/etc/systemd/system.conf.d/edgevpn-kairos.env
LimitNOFILE=49152
ExecStart=edgevpn api --enable-healthchecks
Restart=always
[Install]
WantedBy=multi-user.target`

const edgevpnSystemd string = `[Unit]
Description=EdgeVPN Daemon
After=network.target
[Service]
EnvironmentFile=/etc/systemd/system.conf.d/edgevpn-%i.env
LimitNOFILE=49152
ExecStart=edgevpn
Restart=always
[Install]
WantedBy=multi-user.target`

const EdgeVPNDefaultInstance string = "kairos"

func EdgeVPN(instance, rootDir string) (machine.Service, error) {
	return service.New(service.Spec{
		Name:     "edgevpn",
		Instance: instance,
		Root:     rootDir,
		Init: map[service.Flavor]service.InitSpec{
			service.OpenRC:  {Unit: edgevpnOpenRC},
			service.Systemd: {Unit: edgevpnSystemd},
		},
	})
}

func P2PAPI(rootDir string) (machine.Service, error) {
	return service.New(service.Spec{
		Name: "edgevpn",
		Root: rootDir,
		Init: map[service.Flavor]service.InitSpec{
			service.OpenRC:  {Unit: edgevpnAPIOpenRC},
			service.Systemd: {Unit: edgevpnAPISystemd},
		},
	})
}
