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

// The DevSec SSH baseline is the reference spec we harden against.
// See: https://github.com/dev-sec/ssh-baseline
//
// This test does not translate the baseline into Ginkgo assertions.
// It runs the upstream InSpec profile against a booted Kairos VM via
// cinc-auditor (a FOSS drop-in for chef/inspec, since InSpec >= 5 is
// no longer under an OSS licence). The runner must have cinc-auditor
// installed; the CI job in .github/workflows/reusable-qemu-test.yaml
// takes care of that when the test label is "ssh-hardening".
//
// This test is expected to FAIL on master until the ssh_hardening
// profile lands in kairos-init. That is the point: the pipeline is
// the spec, in TDD order.
var _ = Describe("ssh hardening", Label("ssh-hardening"), func() {
	var vm VM

	BeforeEach(func() {
		_, vm = startVM()
		vm.EventuallyConnects(1200)
	})

	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			serial, _ := os.ReadFile(filepath.Join(vm.StateDir, "serial.log"))
			_ = os.MkdirAll("logs", 0o755)
			_ = os.WriteFile(filepath.Join("logs", "serial.log"), serial, 0o644)
		}
		Expect(vm.Destroy(nil)).ToNot(HaveOccurred())
	})

	It("passes the DevSec ssh-baseline profile", func() {
		sshPort := os.Getenv("SSH_PORT")
		Expect(sshPort).ToNot(BeEmpty(), "SSH_PORT must be set by the CI job so cinc-auditor can reach the VM")

		reportDir := "logs"
		Expect(os.MkdirAll(reportDir, 0o755)).To(Succeed())
		reportPath := filepath.Join(reportDir, "ssh-baseline.json")

		cmd := exec.Command("cinc-auditor", "exec",
			"https://github.com/dev-sec/ssh-baseline",
			"--target", fmt.Sprintf("ssh://%s@127.0.0.1:%s", user(), sshPort),
			"--password", pass(),
			"--reporter", "cli", "json:"+reportPath,
			"--chef-license", "accept-silent",
		)
		cmd.Env = append(os.Environ(), "TERM=dumb")
		out, err := cmd.CombinedOutput()
		GinkgoWriter.Printf("cinc-auditor output:\n%s\n", string(out))

		Expect(err).ToNot(HaveOccurred(),
			"ssh-baseline profile reported failures; see %s for the JSON report", reportPath)
	})
})
