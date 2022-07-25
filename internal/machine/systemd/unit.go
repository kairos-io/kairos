package systemd

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/c3os-io/c3os/internal/utils"
)

type ServiceUnit struct {
	content        string
	name, instance string
	rootdir        string
}

const overrideCmdTemplate string = `
[Service]
ExecStart=
ExecStart=%s
`

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

	if err := ioutil.WriteFile(filepath.Join(s.rootdir, fmt.Sprintf("/etc/systemd/system/%s.service", uname)), []byte(s.content), 0600); err != nil {
		return err
	}

	_, err := utils.SH("systemctl daemon-reload")
	return err
}

func (s ServiceUnit) OverrideCmd(cmd string) error {
	svcDir := filepath.Join(s.rootdir, fmt.Sprintf("/etc/systemd/system/%s.service.d/", s.name))
	os.MkdirAll(svcDir, 0600) //nolint:errcheck

	return ioutil.WriteFile(filepath.Join(svcDir, "override.conf"), []byte(fmt.Sprintf(overrideCmdTemplate, cmd)), 0600)
}

func (s ServiceUnit) Start() error {
	return s.systemctl("start", false)
}

func (s ServiceUnit) Restart() error {
	return s.systemctl("restart", false)
}

func (s ServiceUnit) Enable() error {
	return s.systemctl("enable", false)
}

func (s ServiceUnit) StartBlocking() error {
	return s.systemctl("start", true)
}

func (s ServiceUnit) systemctl(action string, blocking bool) error {
	uname := s.name
	if s.instance != "" {
		uname = fmt.Sprintf("%s@%s", s.name, s.instance)
	}
	args := []string{action}
	if !blocking {
		args = append(args, "--no-block")
	}
	args = append(args, uname)

	_, err := utils.SH(fmt.Sprintf("systemctl %s", strings.Join(args, " ")))
	return err
}
