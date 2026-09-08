package openrc

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kairos-io/kairos/v4/sdk/utils"
)

type ServiceUnit struct {
	content string
	name    string
	rootdir string
	envFile string
}

type ServiceOpts func(*ServiceUnit) error

func WithRoot(n string) ServiceOpts {
	return func(su *ServiceUnit) error {
		su.rootdir = n
		return nil
	}
}

func WithName(n string) ServiceOpts {
	return func(su *ServiceUnit) error {
		su.name = n
		return nil
	}
}

func WithUnitContent(n string) ServiceOpts {
	return func(su *ServiceUnit) error {
		su.content = n
		return nil
	}
}

// WithEnvFile sets the file that OverrideCmd writes command_args to.
//
// openrc has no equivalent of a systemd drop-in, so the only way to change the
// arguments of a service without rewriting its script is to set command_args in
// a file that the script sources. Which file that is belongs to the service:
// k3s reads /etc/rancher/k3s/<unit>.env, k0s reads /etc/k0s/<unit>.env. Pass the
// path the unit actually sources. Without it, OverrideCmd writes to openrc's own
// /etc/conf.d/<name>.
func WithEnvFile(n string) ServiceOpts {
	return func(su *ServiceUnit) error {
		su.envFile = n
		return nil
	}
}

func NewService(opts ...ServiceOpts) (ServiceUnit, error) {
	s := &ServiceUnit{}
	for _, o := range opts {
		if err := o(s); err != nil {
			return *s, err
		}
	}
	return *s, nil
}

func (s ServiceUnit) WriteUnit() error {
	uname := s.name

	if err := os.WriteFile(filepath.Join(s.rootdir, fmt.Sprintf("/etc/init.d/%s", uname)), []byte(s.content), 0755); err != nil {
		return err
	}

	return nil
}

// EnvFile returns the file OverrideCmd writes command_args to.
func (s ServiceUnit) EnvFile() string {
	if s.envFile != "" {
		return filepath.Join(s.rootdir, s.envFile)
	}

	return filepath.Join(s.rootdir, fmt.Sprintf("/etc/conf.d/%s", s.name))
}

// OverrideCmd changes the arguments the service runs with.
//
// cmd is the full command line, binary included, so that callers can use the
// same string for openrc and for systemd. openrc keeps the binary in command
// and only the arguments in command_args, so the leading binary is stripped
// here.
func (s ServiceUnit) OverrideCmd(cmd string) error {
	cmd = strings.TrimSpace(cmd)
	bin, args, _ := strings.Cut(cmd, " ")
	if bin == "" {
		return fmt.Errorf("no command to override for service %s", s.name)
	}

	env := map[string]string{
		"command_args": fmt.Sprintf("%s >>/var/log/%s.log 2>&1", strings.TrimSpace(args), s.name),
	}

	return utils.WriteEnv(s.EnvFile(), env)
}

func (s ServiceUnit) Start() error {
	out, err := utils.SH(fmt.Sprintf("/etc/init.d/%s start", s.name))
	if err != nil {
		return fmt.Errorf("failed starting service: %s. %s (%w)", s.name, out, err)
	}
	return nil
}

func (s ServiceUnit) Restart() error {
	out, err := utils.SH(fmt.Sprintf("/etc/init.d/%s restart", s.name))
	if err != nil {
		return fmt.Errorf("failed restarting service: %s. %s (%w)", s.name, out, err)
	}
	return nil
}

func (s ServiceUnit) Enable() error {
	_, err := utils.SH(fmt.Sprintf("ln -sf /etc/init.d/%s /etc/runlevels/default/%s", s.name, s.name))
	return err
}

func (s ServiceUnit) StartBlocking() error {
	return s.Start()
}
