package validation_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// evalRecoveryGuard runs a stage's `if` the way yip does, with the
// recovery_mode sentinel present or absent. An empty guard is "always run",
// which is what `sh -c ""` already reports, so a stage with no `if` at all
// comes back true for both boots.
func evalRecoveryGuard(expr string, recovery bool) bool {
	root := GinkgoT().TempDir()

	sentinel := filepath.Join(root, "recovery_mode")
	if recovery {
		Expect(os.WriteFile(sentinel, nil, 0644)).To(Succeed())
	}
	expr = strings.ReplaceAll(expr, "/run/cos/recovery_mode", sentinel)

	err := exec.Command("sh", "-c", expr).Run()
	if err == nil {
		return true
	}
	_, isExit := err.(*exec.ExitError)
	Expect(isExit).To(BeTrue(), "guard failed to run: %v", err)
	return false
}

// stageWritingFor returns the stage for the given service manager that writes
// the named file, so a spec can name the stage by what it installs rather than
// by its position in the file.
func stageWritingFor(stages []guardStage, serviceManager, path string) guardStage {
	for _, s := range stages {
		if s.OnlyServiceManager != serviceManager {
			continue
		}
		for _, f := range s.Files {
			if f.Path == path {
				return s
			}
		}
	}
	Fail("no " + serviceManager + " stage writes " + path)
	return guardStage{}
}

// kairos-agent start is the provider agent: it runs the FirstBoot hooks,
// installs bundles, rewrites the OEM grubenv through GrubFirstBootOptions and
// publishes the bootstrap event. agent.Run has no recovery check of its own,
// and in recovery /usr/local is the tmpfs overlay, so the firstboot sentinel
// is never found and the hooks run on every recovery boot. 02_agent.yaml keeps
// the agent off a recovery boot on systemd; the openrc side has to do the same.
var _ = Describe("Bundled cloudconfigs agent recovery guard", func() {
	var systemd, openrcWrite, openrcEnable guardStage

	BeforeEach(func() {
		systemd = stageEnabling(readStages("02_agent.yaml"), "kairos-agent")

		openrc := readStages("09_openrc_services.yaml")
		openrcWrite = stageWritingFor(openrc, "openrc", "/etc/init.d/kairos-agent")
		openrcEnable = stageRunningFor(openrc, "openrc",
			"ln -sf ../../init.d/kairos-agent /etc/runlevels/default/kairos-agent")
	})

	It("keeps the agent off a recovery boot on systemd", func() {
		Expect(evalRecoveryGuard(systemd.If, true)).To(BeFalse())
		Expect(evalRecoveryGuard(systemd.If, false)).To(BeTrue())
	})

	// Both openrc stages are separate `if`s from the systemd one, so pin them
	// to the same answer rather than to the same text.
	It("guards the openrc agent exactly as the systemd one", func() {
		for _, recovery := range []bool{true, false} {
			Expect(evalRecoveryGuard(openrcWrite.If, recovery)).
				To(Equal(evalRecoveryGuard(systemd.If, recovery)),
					"writing /etc/init.d/kairos-agent, recovery=%v", recovery)
			Expect(evalRecoveryGuard(openrcEnable.If, recovery)).
				To(Equal(evalRecoveryGuard(systemd.If, recovery)),
					"linking kairos-agent into a runlevel, recovery=%v", recovery)
		}
	})

	// A service that is written but never linked never starts, and a link to a
	// script that was never written is a dangling symlink openrc warns about.
	// The two stages have to agree on every boot, not only on recovery.
	It("writes and links the openrc agent under the same condition", func() {
		for _, recovery := range []bool{true, false} {
			Expect(evalRecoveryGuard(openrcEnable.If, recovery)).
				To(Equal(evalRecoveryGuard(openrcWrite.If, recovery)), "recovery=%v", recovery)
		}
	})

	// The other openrc services are deliberately unguarded: cos-setup-boot,
	// cos-setup-network and cos-setup-reconcile run the same stages recovery
	// needs, and 50_recovery.yaml's boot stage is one of them.
	It("leaves the cos-setup services running in recovery", func() {
		openrc := readStages("09_openrc_services.yaml")
		for _, service := range []string{"cos-setup-boot", "cos-setup-network", "cos-setup-reconcile"} {
			stage := stageWritingFor(openrc, "openrc", "/etc/init.d/"+service)
			Expect(evalRecoveryGuard(stage.If, true)).To(BeTrue(), service)
		}
	})
})
