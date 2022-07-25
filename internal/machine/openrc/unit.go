package openrc

import (
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strings"

	"github.com/c3os-io/c3os/internal/utils"
)

type ServiceUnit struct {
	content string
	name    string
	rootdir string
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

	if err := ioutil.WriteFile(filepath.Join(s.rootdir, fmt.Sprintf("/etc/init.d/%s", uname)), []byte(s.content), 0755); err != nil {
		return err
	}

	return nil
}

// TODO: This is too much k3s specific
func (s ServiceUnit) OverrideCmd(cmd string) error {
	k3sbin := utils.K3sBin()
	if k3sbin == "" {
		return fmt.Errorf("no k3s binary found (?)")
	}
	cmd = strings.ReplaceAll(cmd, k3sbin+" ", "")
	envFile := filepath.Join(s.rootdir, fmt.Sprintf("/etc/rancher/k3s/%s.env", s.name))
	env := make(map[string]string)
	env["command_args"] = fmt.Sprintf("%s >>/var/log/%s.log 2>&1", cmd, s.name)

	return utils.WriteEnv(envFile, env)
}

func (s ServiceUnit) Start() error {
	_, err := utils.SH(fmt.Sprintf("/etc/init.d/%s start", s.name))
	return err
}

func (s ServiceUnit) Restart() error {
	_, err := utils.SH(fmt.Sprintf("/etc/init.d/%s restart", s.name))
	return err
}

func (s ServiceUnit) Enable() error {
	_, err := utils.SH(fmt.Sprintf("ln -sf /etc/init.d/%s /etc/runlevels/default/%s", s.name, s.name))
	return err
}

func (s ServiceUnit) StartBlocking() error {
	return s.Start()
}
