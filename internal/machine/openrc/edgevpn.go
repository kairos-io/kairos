package openrc

const EdgevpnUnit string = `#!/sbin/openrc-run

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
if [ -f /etc/systemd/system.conf.d/edgevpn-c3os.env ]; then source /etc/systemd/system.conf.d/edgevpn-c3os.env; fi
set +o allexport`
