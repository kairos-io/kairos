package systemd

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/mudler/c3os/installer/utils"
)

type ServiceUnit struct {
	content        string
	name, instance string
}

const overrideCmdTemplate string = `
[Service]
ExecStart=
ExecStart=%s
`

type ServiceOpts func(*ServiceUnit) error

func WithName(n string) ServiceOpts {
	return func(su *ServiceUnit) error {
		su.name = n
		return nil
	}
}

func WithInstance(n string) ServiceOpts {
	return func(su *ServiceUnit) error {
		su.instance = n
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
	if s.instance != "" {
		uname = fmt.Sprintf("%s@", s.name)
	}
	return ioutil.WriteFile(fmt.Sprintf("/etc/systemd/system/%s.service", uname), []byte(s.content), 0600)
}

func (s ServiceUnit) OverrideCmd(cmd string) error {
	svcDir := fmt.Sprintf("/etc/systemd/system/%s.service.d/", s.name)
	os.MkdirAll(svcDir, 0600)

	return ioutil.WriteFile(filepath.Join(svcDir, "override.conf"), []byte(fmt.Sprintf(overrideCmdTemplate, cmd)), 0600)
}

func (s ServiceUnit) Start() error {
	uname := s.name
	if s.instance != "" {
		uname = fmt.Sprintf("%s@%s", s.name, s.instance)
	}
	_, err := utils.SH(fmt.Sprintf("systemctl start --no-block %s", uname))
	return err
}

func (s ServiceUnit) Enable() error {
	uname := s.name
	if s.instance != "" {
		uname = fmt.Sprintf("%s@%s", s.name, s.instance)
	}
	_, err := utils.SH(fmt.Sprintf("systemctl enable %s", uname))
	return err
}

func (s ServiceUnit) StartBlocking() error {
	uname := s.name
	if s.instance != "" {
		uname = fmt.Sprintf("%s@%s", s.name, s.instance)
	}
	_, err := utils.SH(fmt.Sprintf("systemctl start %s", uname))
	return err
}
