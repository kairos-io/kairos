package service

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/kairos-io/kairos/v4/sdk/utils"
)

type openrcBackend struct{}

func (openrcBackend) Flavor() Flavor { return OpenRC }

func (openrcBackend) New(spec Spec) (Service, error) {
	return openrcService{spec: spec, init: spec.For(OpenRC)}, nil
}

type openrcService struct {
	spec Spec
	init InitSpec
}

func (s openrcService) Name() string { return s.spec.Name }

// EnvFile has no default on openrc.
//
// openrc sources /etc/conf.d/<name> before it runs the script body, so a
// command_args written there is overwritten by any assignment the script makes
// itself, which is what the scripts Kairos ships do. There is no path that
// works for every service, so a service that wants its arguments overridden has
// to name the file its own script sources.
func (s openrcService) EnvFile() string { return s.init.EnvFile }

func (s openrcService) WriteUnit() error {
	unit, err := unitToWrite(s.spec.Name, s.init, s.EnvFile())
	if err != nil {
		return err
	}

	return writeFile(filepath.Join(s.spec.Root, "/etc/init.d", s.spec.Name), unit, 0755)
}

// OverrideCmd changes the arguments the service runs with.
//
// openrc keeps the binary in command and only the arguments in command_args, so
// the leading binary is stripped from cmd here. That way callers can hand the
// same whole command line to every init system.
func (s openrcService) OverrideCmd(cmd string) error {
	cmd = strings.TrimSpace(cmd)
	bin, args, _ := strings.Cut(cmd, " ")
	if bin == "" {
		return fmt.Errorf("no command to override for service %s", s.spec.Name)
	}

	return s.SetEnv(map[string]string{"command_args": strings.TrimSpace(args)})
}

func (s openrcService) SetEnv(env map[string]string) error {
	if s.EnvFile() == "" {
		return fmt.Errorf("no env file configured for service %s", s.spec.Name)
	}

	return setEnv(filepath.Join(s.spec.Root, s.EnvFile()), env)
}

func (s openrcService) Start() error         { return s.rc("start") }
func (s openrcService) StartBlocking() error { return s.rc("start") }
func (s openrcService) Stop() error          { return s.rc("stop") }
func (s openrcService) Restart() error       { return s.rc("restart") }

func (s openrcService) Enable() error {
	_, err := utils.SH(fmt.Sprintf("ln -sf /etc/init.d/%s /etc/runlevels/default/%s", s.spec.Name, s.spec.Name))
	return err
}

func (s openrcService) Disable() error {
	_, err := utils.SH(fmt.Sprintf("rm -f /etc/runlevels/default/%s", s.spec.Name))
	return err
}

func (s openrcService) rc(action string) error {
	out, err := utils.SH(fmt.Sprintf("/etc/init.d/%s %s", s.spec.Name, action))
	if err != nil {
		return fmt.Errorf("failed to %s service %s: %s (%w)", action, s.spec.Name, out, err)
	}
	return nil
}
