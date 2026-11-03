package machine

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/denisbrodbeck/machineid"
	"github.com/kairos-io/kairos/v4/sdk/machine/service"
	"github.com/mudler/yip/pkg/console"
	"github.com/mudler/yip/pkg/executor"
	"github.com/twpayne/go-vfs/v4"

	"github.com/kairos-io/kairos/v4/sdk/types/logger"
	"github.com/kairos-io/kairos/v4/sdk/utils"
)

// Service is a system service, whatever the init system underneath.
//
// The interface and the implementations live in sdk/machine/service; this alias
// is here so existing callers keep compiling.
type Service = service.Service

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

// consoleSwitch stands in for a getty service on an init system that runs no
// getty unit we can address. Switching the console is then the whole of what
// starting one can mean.
type consoleSwitch struct {
	service.Noop
}

func (consoleSwitch) Start() error {
	utils.SH("chvt 2") //nolint:errcheck
	return nil
}

// Getty returns the service that owns a virtual console.
//
// On openrc the tty asked for is ignored and the console switches to tty2, as
// it did before this was a generic service. That looks wrong next to the
// systemd side, but changing which console Kairos switches to belongs in its
// own change, with a reason.
func Getty(i int) (Service, error) {
	if service.Detect() == service.OpenRC {
		return consoleSwitch{Noop: service.Noop{ServiceName: "getty"}}, nil
	}

	return service.New(service.Spec{
		Name:     "getty",
		Instance: fmt.Sprintf("tty%d", i),
	})
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
	return os.WriteFile(fmt.Sprintf("/usr/local/.kairos/sentinel_%s", f), []byte{}, os.ModePerm)
}

func SentinelExist(f string) bool {
	if _, err := os.Stat(fmt.Sprintf("/usr/local/.kairos/sentinel_%s", f)); err == nil {
		return true
	}
	return false
}

// runStage drives yip in this process over a single source: either a
// cloud-config document or a path to one.
//
// It does not shell out. The command these two helpers used to run,
// "elemental run-stage", is not in any Kairos image: kairos-init/pkg/bundled
// embeds kairos, provider-kairos, kairos-installer and edgevpn, and no package
// list installs elemental. Every other stage call in the tree goes through
// "kairos-agent run-stage", which takes the stage as a positional argument and
// cannot read a document from standard input, so it is not a drop-in either.
//
// Only the source it is handed runs. /oem and the other default cloud-config
// directories are deliberately left out: the caller asked for one document,
// and running the system's whole config as a side effect of that would be a
// surprise. Use the agent's RunStage when you want the default paths.
//
// runStage is a variable so a spec can exercise the callers without a yip run.
var runStage = func(stage, source string) error {
	e := executor.NewExecutor(executor.WithLogger(logger.NewKairosLogger("sdk", "info", false)))
	return e.Run(stage, vfs.OSFS, console.NewStandardConsole(), source)
}

// ExecuteInlineCloudConfig runs one stage of the cloud-config document it is
// given against the running system.
func ExecuteInlineCloudConfig(cloudConfig, stage string) error {
	return runStage(stage, cloudConfig)
}

// ExecuteCloudConfig runs one stage of the cloud-config file at the given path
// against the running system.
func ExecuteCloudConfig(file, stage string) error {
	return runStage(stage, file)
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
