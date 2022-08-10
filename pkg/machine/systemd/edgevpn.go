package systemd

const EdgevpnUnit string = `[Unit]
Description=EdgeVPN Daemon
After=network.target
[Service]
EnvironmentFile=/etc/systemd/system.conf.d/edgevpn-%i.env
LimitNOFILE=49152
ExecStart=edgevpn
Restart=always
[Install]
WantedBy=multi-user.target`
