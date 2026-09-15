package service

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/kairos-io/kairos/v4/sdk/utils"
)

const overrideCmdTemplate string = `
[Service]
ExecStart=
ExecStart=%s
`

type systemdBackend struct{}

func (systemdBackend) Flavor() Flavor { return Systemd }

func (systemdBackend) New(spec Spec) (Service, error) {
	return systemdService{spec: spec, init: spec.For(Systemd)}, nil
}

type systemdService struct {
	spec Spec
	init InitSpec
}

func (s systemdService) Name() string { return s.spec.Name }

// unitName is the name systemctl takes: name@instance for a templated unit.
func (s systemdService) unitName() string {
	if s.spec.Instance == "" {
		return s.spec.Name
	}
	return fmt.Sprintf("%s@%s", s.spec.Name, s.spec.Instance)
}

// fileName is the name the unit is written under, which for a templated unit is
// the template itself and carries no instance.
func (s systemdService) fileName() string {
	if s.spec.Instance == "" {
		return s.spec.Name
	}
	return fmt.Sprintf("%s@", s.spec.Name)
}

// EnvFile defaults to /etc/sysconfig/<name>, which is where systemd
// distributions keep the EnvironmentFile of a packaged unit.
func (s systemdService) EnvFile() string {
	if s.init.EnvFile != "" {
		return s.init.EnvFile
	}
	return filepath.Join("/etc/sysconfig", s.spec.Name)
}

func (s systemdService) WriteUnit() error {
	unit, err := unitToWrite(s.spec.Name, s.init, s.EnvFile())
	if err != nil {
		return err
	}

	path := filepath.Join(s.spec.Root, "/etc/systemd/system", s.fileName()+".service")
	if err := writeFile(path, unit, 0600); err != nil {
		return err
	}

	if s.spec.NoReload {
		return nil
	}

	_, err = utils.SH("systemctl daemon-reload")
	return err
}

// OverrideCmd writes a drop-in that replaces ExecStart. systemd passes
// arguments in the unit rather than in the env file, so the env file is not
// involved.
func (s systemdService) OverrideCmd(cmd string) error {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return fmt.Errorf("no command to override for service %s", s.spec.Name)
	}

	dir := filepath.Join(s.spec.Root, "/etc/systemd/system", s.spec.Name+".service.d")
	return writeFile(filepath.Join(dir, "override.conf"), fmt.Sprintf(overrideCmdTemplate, cmd), 0600)
}

func (s systemdService) SetEnv(env map[string]string) error {
	return setEnv(filepath.Join(s.spec.Root, s.EnvFile()), env)
}

func (s systemdService) Start() error         { return s.systemctl("start", false) }
func (s systemdService) StartBlocking() error { return s.systemctl("start", true) }
func (s systemdService) Stop() error          { return s.systemctl("stop", false) }
func (s systemdService) Restart() error       { return s.systemctl("restart", false) }
func (s systemdService) Enable() error        { return s.systemctl("enable", false) }
func (s systemdService) Disable() error       { return s.systemctl("disable", false) }

func (s systemdService) systemctl(action string, blocking bool) error {
	args := []string{action}
	if !blocking {
		args = append(args, "--no-block")
	}
	args = append(args, s.unitName())

	_, err := utils.SH(fmt.Sprintf("systemctl %s", strings.Join(args, " ")))
	return err
}
