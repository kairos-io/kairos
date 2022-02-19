package machine

import (
	"fmt"

	"github.com/c3os-io/c3os/cli/machine/openrc"
	"github.com/c3os-io/c3os/cli/machine/systemd"

	"github.com/c3os-io/c3os/cli/utils"
)

type Service interface {
	WriteUnit() error
	Start() error
	OverrideCmd(string) error
	Enable() error
	Restart() error
	SetEnvFile(es string) error
}

func EdgeVPN(instance, rootDir string) (Service, error) {
	switch utils.Flavor() {
	case "alpine":
		return openrc.NewService(
			openrc.WithName("edgevpn"),
			openrc.WithUnitContent(openrc.EdgevpnUnit),
			openrc.WithRoot(rootDir),
		)
	default:
		return systemd.NewService(
			systemd.WithName("edgevpn"),
			systemd.WithInstance(instance),
			systemd.WithUnitContent(systemd.EdgevpnUnit),
			systemd.WithRoot(rootDir),
		)
	}
}

const EdgeVPNDefaultInstance string = "c3os"

type fakegetty struct{}

func (fakegetty) Restart() error           { return nil }
func (fakegetty) Enable() error            { return nil }
func (fakegetty) OverrideCmd(string) error { return nil }
func (fakegetty) SetEnvFile(string) error  { return nil }
func (fakegetty) WriteUnit() error         { return nil }
func (fakegetty) Start() error {
	utils.SH("chvt 2")
	return nil
}

func Getty(i int) (Service, error) {
	switch utils.Flavor() {
	case "alpine":
		return &fakegetty{}, nil
	default:
		return systemd.NewService(
			systemd.WithName("getty"),
			systemd.WithInstance(fmt.Sprintf("tty%d", i)),
		)
	}
}

func K3s() (Service, error) {
	switch utils.Flavor() {
	case "alpine":
		return openrc.NewService(
			openrc.WithName("k3s"),
		)
	default:
		return systemd.NewService(
			systemd.WithName("k3s"),
		)
	}
}

func K3sAgent() (Service, error) {
	switch utils.Flavor() {
	case "alpine":
		return openrc.NewService(
			openrc.WithName("k3s-agent"),
		)
	default:
		return systemd.NewService(
			systemd.WithName("k3s-agent"),
		)
	}
}
