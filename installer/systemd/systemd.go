package systemd

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/hashicorp/go-multierror"
	"github.com/joho/godotenv"
)

type Unit string

const EdgeVPN Unit = `[Unit]
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

const override string = `
[Service]
ExecStart=
ExecStart=%s
`

func systemdWriteUnit(s, content string) error {
	return ioutil.WriteFile(fmt.Sprintf("/etc/systemd/system/%s.service", s), []byte(content), 0600)
}

func OverrideServiceCmd(service, cmd string) error {
	svcDir := fmt.Sprintf("/etc/systemd/system/%s.service.d/", service)
	os.MkdirAll(svcDir, 0600)

	return ioutil.WriteFile(filepath.Join(svcDir, "override.conf"), []byte(fmt.Sprintf(override, cmd)), 0600)
}

func WriteEnv(envFile string, config map[string]string) error {
	content, _ := ioutil.ReadFile(envFile)
	env, _ := godotenv.Unmarshal(string(content))

	for key, val := range config {
		env[key] = val
	}

	return godotenv.Write(env, envFile)
}

func (u Unit) Prepare(opts map[string]string) (err error) {

	// Setup systemd unit and starts it
	err = multierror.Append(WriteEnv("/etc/systemd/system.conf.d/edgevpn-c3os.env", opts))
	err = multierror.Append(systemdWriteUnit("edgevpn@", string(u)))
	err = multierror.Append(StartUnit("edgevpn@c3os"))
	err = multierror.Append(EnableUnit("edgevpn@c3os"))

	return
}

func StartUnit(s string) error {
	return exec.Command("/bin/sh", "-c", "systemctl", "start", "--no-block", s).Run()
}

func EnableUnit(s string) error {
	return exec.Command("/bin/sh", "-c", "systemctl", "enable", s).Run()
}

func StartUnitBlocking(s string) error {
	return exec.Command("/bin/sh", "-c", "systemctl", "start", s).Run()
}
