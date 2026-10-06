package mos_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/spectrocloud/peg/matcher"
)

// cisProfile is the InSpec profile for the CIS Distribution Independent
// Linux Benchmark v2.0.0, the benchmark kairos-init's cisHardening step
// targets. Pinned so an upstream change cannot break CI without a bump.
const cisProfile = "https://github.com/dev-sec/cis-dil-benchmark/archive/refs/tags/0.4.12.tar.gz"

// Runs CIS DIL Level 1 against an installed Kairos system. kairos-init
// hardens every image by default, so the default install is the system
// under test. cinc-auditor (FOSS InSpec) runs on the host and reaches the
// VM over SSH; nothing is installed in the image. Controls that cannot
// apply to Kairos are waived in assets/cis-dil-waivers.yaml with a reason.
var _ = Describe("cis benchmark", Label("cis"), func() {
	var vm VM

	BeforeEach(func() {
		_, vm = startVM()
		vm.EventuallyConnects(1200)
	})

	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			serial, _ := os.ReadFile(filepath.Join(vm.StateDir, "serial.log"))
			_ = os.MkdirAll("logs", os.ModePerm|os.ModeDir)
			_ = os.WriteFile(filepath.Join("logs", "serial.log"), serial, os.ModePerm)
			gatherLogs(vm)
		}
		Expect(vm.Destroy(nil)).ToNot(HaveOccurred())
	})

	It("passes CIS DIL Level 1", func() {
		testInstall(`#cloud-config
install:
  grub_options:
    extra_cmdline: "rd.immucore.debug"
users:
- name: "kairos"
  passwd: "kairos"
  groups:
    - "admin"
`, vm)

		By("checking we are on the installed system", func() {
			out, err := vm.Sudo("test -e /run/cos/active_mode && echo ACTIVE")
			Expect(err).ToNot(HaveOccurred(), out)
			Expect(out).To(ContainSubstring("ACTIVE"), out)
		})

		reportDir := "logs"
		Expect(os.MkdirAll(reportDir, 0o755)).To(Succeed())
		reportPath := filepath.Join(reportDir, "cis-dil.json")
		cliLog := filepath.Join(reportDir, "cis-dil.cli.log")

		cmd := exec.Command("cinc-auditor", "exec", cisProfile,
			"--target", fmt.Sprintf("ssh://%s@127.0.0.1:%s", user(), vm.SSHPort()),
			"--password", pass(),
			"--sudo", "--sudo-password", pass(),
			"--input", "cis_level=1",
			// `run: false` waivers skip the control, so it does not count
			// towards the result. Not combined with
			// --filter-waived-controls, which crashes cinc-auditor 7.1.7
			// (see ssh_hardening_test.go).
			"--waiver-file", filepath.Join("assets", "cis-dil-waivers.yaml"),
			"--reporter", "cli", "json:"+reportPath,
			"--chef-license", "accept-silent",
		)
		cmd.Env = append(os.Environ(), "TERM=dumb")
		out, err := cmd.CombinedOutput()
		_ = os.WriteFile(cliLog, out, 0o644)
		GinkgoWriter.Printf("cinc-auditor output:\n%s\n", string(out))

		// 0: all passed. 101: passed with skips (waived controls). 100: at
		// least one control failed. Anything else is a harness error.
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 101 {
			err = nil
		}
		if err != nil {
			summarizeFailedControls("cis-dil", reportPath)
		}
		Expect(err).ToNot(HaveOccurred(),
			"CIS DIL L1 reported failures; failed controls are printed above; %s and %s are attached as artifacts",
			reportPath, cliLog)
	})
})
