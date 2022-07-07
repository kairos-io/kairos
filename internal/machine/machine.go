package machine

import (
	"fmt"
	"os"

	"github.com/c3os-io/c3os/internal/machine/openrc"
	"github.com/c3os-io/c3os/internal/machine/systemd"
	"github.com/denisbrodbeck/machineid"

	"github.com/c3os-io/c3os/internal/utils"
)

type Service interface {
	WriteUnit() error
	Start() error
	OverrideCmd(string) error
	Enable() error
	Restart() error
}

func EdgeVPN(instance, rootDir string) (Service, error) {
	if utils.IsOpenRCBased() {
		return openrc.NewService(
			openrc.WithName("edgevpn"),
			openrc.WithUnitContent(openrc.EdgevpnUnit),
			openrc.WithRoot(rootDir),
		)
	} else {
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
	if utils.IsOpenRCBased() {
		return &fakegetty{}, nil
	} else {
		return systemd.NewService(
			systemd.WithName("getty"),
			systemd.WithInstance(fmt.Sprintf("tty%d", i)),
		)
	}
}

func K3s() (Service, error) {
	if utils.IsOpenRCBased() {
		return openrc.NewService(
			openrc.WithName("k3s"),
		)
	} else {
		return systemd.NewService(
			systemd.WithName("k3s"),
		)
	}
}

func K3sAgent() (Service, error) {
	if utils.IsOpenRCBased() {
		return openrc.NewService(
			openrc.WithName("k3s-agent"),
		)
	} else {
		return systemd.NewService(
			systemd.WithName("k3s-agent"),
		)
	}
}

func K3sEnvUnit(unit string) string {
	if utils.IsOpenRCBased() {
		return fmt.Sprintf("/etc/rancher/k3s/%s.env", unit)
	} else {
		return fmt.Sprintf("/etc/sysconfig/%s", unit)
	}
}

func UUID() string {
	if os.Getenv("UUID") != "" {
		return os.Getenv("UUID")
	}
	id, _ := machineid.ID()
	hostname, _ := os.Hostname()
	return fmt.Sprintf("%s-%s", id, hostname)
}
