package machine

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"

	"github.com/denisbrodbeck/machineid"
	"github.com/kairos-io/kairos/pkg/machine/openrc"
	"github.com/kairos-io/kairos/pkg/machine/systemd"

	"github.com/kairos-io/kairos/pkg/utils"
)

type Service interface {
	WriteUnit() error
	Start() error
	OverrideCmd(string) error
	Enable() error
	Restart() error
}

const (
	PassiveBoot  = "passive"
	ActiveBoot   = "active"
	RecoveryBoot = "recovery"
	LiveCDBoot   = "liveCD"
	NetBoot      = "netboot"
	UnknownBoot  = "unknown"
)

// BootFrom returns the booting partition of the SUT.
func BootFrom() string {
	out, err := utils.SH("cat /proc/cmdline")
	if err != nil {
		return UnknownBoot
	}
	switch {
	case strings.Contains(out, "COS_ACTIVE"):
		return ActiveBoot
	case strings.Contains(out, "COS_PASSIVE"):
		return PassiveBoot
	case strings.Contains(out, "COS_RECOVERY"), strings.Contains(out, "COS_SYSTEM"):
		return RecoveryBoot
	case strings.Contains(out, "live:CDLABEL"):
		return LiveCDBoot
	case strings.Contains(out, "netboot"):
		return NetBoot
	default:
		return UnknownBoot
	}
}

type fakegetty struct{}

func (fakegetty) Restart() error           { return nil }
func (fakegetty) Enable() error            { return nil }
func (fakegetty) OverrideCmd(string) error { return nil }
func (fakegetty) SetEnvFile(string) error  { return nil }
func (fakegetty) WriteUnit() error         { return nil }
func (fakegetty) Start() error {
	utils.SH("chvt 2") //nolint:errcheck
	return nil
}

func Getty(i int) (Service, error) {
	if utils.IsOpenRCBased() {
		return &fakegetty{}, nil
	}

	return systemd.NewService(
		systemd.WithName("getty"),
		systemd.WithInstance(fmt.Sprintf("tty%d", i)),
	)
}

func K3s() (Service, error) {
	if utils.IsOpenRCBased() {
		return openrc.NewService(
			openrc.WithName("k3s"),
		)
	}

	return systemd.NewService(
		systemd.WithName("k3s"),
	)
}

func K3sAgent() (Service, error) {
	if utils.IsOpenRCBased() {
		return openrc.NewService(
			openrc.WithName("k3s-agent"),
		)
	}

	return systemd.NewService(
		systemd.WithName("k3s-agent"),
	)
}

func K3sEnvUnit(unit string) string {
	if utils.IsOpenRCBased() {
		return fmt.Sprintf("/etc/rancher/k3s/%s.env", unit)
	}

	return fmt.Sprintf("/etc/sysconfig/%s", unit)
}

func UUID() string {
	if os.Getenv("UUID") != "" {
		return os.Getenv("UUID")
	}
	id, _ := machineid.ID()
	hostname, _ := os.Hostname()
	return fmt.Sprintf("%s-%s", id, hostname)
}

func CreateSentinel(f string) error {
	return ioutil.WriteFile(fmt.Sprintf("/usr/local/.kairos/sentinel_%s", f), []byte{}, os.ModePerm)
}

func SentinelExist(f string) bool {
	if _, err := os.Stat(fmt.Sprintf("/usr/local/.kairos/sentinel_%s", f)); err == nil {
		return true
	}
	return false
}

func ExecuteInlineCloudConfig(cloudConfig, stage string) error {
	_, err := utils.ShellSTDIN(cloudConfig, fmt.Sprintf("elemental run-stage -s %s -", stage))
	return err
}

func ExecuteCloudConfig(file, stage string) error {
	_, err := utils.SH(fmt.Sprintf("elemental run-stage -s %s %s", stage, file))
	return err
}

func FindCommand(def string, options []string) string {
	for _, p := range options {
		path, err := exec.LookPath(p)
		if err == nil {
			return path
		}
	}

	// Otherwise return default
	return def
}
