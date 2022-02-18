package systemd

import (
	"fmt"
)

const edgevpnUnit string = `[Unit]
Description=EdgeVPN Daemon
After=network.target
[Service]
EnvironmentFile=/etc/systemd/system.conf.d/edgevpn-%i.env
LimitNOFILE=49152
ExecStartPre=-/bin/sh -c "sysctl -w net.core.rmem_max=2500000"
ExecStart=edgevpn
Restart=always
[Install]
WantedBy=multi-user.target`

func EdgeVPN(instance, rootDir string) (ServiceUnit, error) {
	return NewService(WithName("edgevpn"), WithInstance(instance), WithUnitContent(edgevpnUnit), WithRoot(rootDir))
}

const EdgeVPNDefaultInstance string = "c3os"

func Getty(i int) (ServiceUnit, error) {
	return NewService(WithName("getty"), WithInstance(fmt.Sprintf("tty%d", i)))
}
