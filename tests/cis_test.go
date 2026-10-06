package mos_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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

		By("checking the controls InSpec cannot evaluate on Hadron", func() {
			// InSpec has no package or service support for Hadron, so these
			// controls come back skipped, which exit 101 would count as a
			// pass. Each one has to be checked on the guest instead.
			unchecked := []string{}
			for _, id := range unsupportedControls(reportPath) {
				if _, ok := cisGuestChecks[id]; !ok {
					unchecked = append(unchecked, id)
				}
			}
			Expect(unchecked).To(BeEmpty(),
				"controls skipped as unsupported on this OS with no guest check in cisGuestChecks and no waiver")

			for id, check := range cisGuestChecks {
				out, err := vm.Sudo(check)
				Expect(err).ToNot(HaveOccurred(), "%s guest check failed: %s\n%s", id, check, out)
			}
		})
	})
})

// cisGuestChecks covers the CIS DIL L1 controls that InSpec skips on Hadron
// because its package and service resources do not support the OS. Each check
// passes (exit 0) when the system meets the control. Services must not be
// enabled (2.1, 2.2) and clients must not be installed (2.3).
var cisGuestChecks = map[string]string{
	"cis-dil-benchmark-2.1.10": notEnabled("xinetd"),
	"cis-dil-benchmark-2.2.3":  notEnabled("avahi-daemon", "avahi-daemon.socket"),
	"cis-dil-benchmark-2.2.4":  notEnabled("cups", "cups.socket"),
	"cis-dil-benchmark-2.2.5":  notEnabled("dhcpd", "isc-dhcp-server", "kea-dhcp4"),
	"cis-dil-benchmark-2.2.6":  notEnabled("slapd"),
	"cis-dil-benchmark-2.2.7":  notEnabled("nfs-server", "rpcbind", "rpcbind.socket"),
	"cis-dil-benchmark-2.2.8":  notEnabled("named", "bind9"),
	"cis-dil-benchmark-2.2.9":  notEnabled("vsftpd"),
	"cis-dil-benchmark-2.2.10": notEnabled("httpd", "apache2", "nginx", "lighttpd"),
	"cis-dil-benchmark-2.2.11": notEnabled("dovecot", "cyrus-imapd"),
	"cis-dil-benchmark-2.2.12": notEnabled("smb", "smbd"),
	"cis-dil-benchmark-2.2.13": notEnabled("squid"),
	"cis-dil-benchmark-2.2.14": notEnabled("snmpd"),
	"cis-dil-benchmark-2.2.16": notEnabled("rsync", "rsyncd"),
	"cis-dil-benchmark-2.2.17": notEnabled("ypserv"),
	"cis-dil-benchmark-2.3.1":  notInstalled("ypbind"),
	"cis-dil-benchmark-2.3.2":  notInstalled("rsh", "rlogin", "rcp"),
	"cis-dil-benchmark-2.3.3":  notInstalled("talk"),
	"cis-dil-benchmark-2.3.4":  notInstalled("telnet"),
	"cis-dil-benchmark-2.3.5":  notInstalled("ldapsearch", "ldapadd"),
	// 3.5.3: a firewall tool is there for whoever sets a policy.
	"cis-dil-benchmark-3.5.3": "command -v iptables >/dev/null || command -v nft >/dev/null",
}

func notEnabled(units ...string) string {
	return fmt.Sprintf("for u in %s; do systemctl is-enabled --quiet \"$u\" 2>/dev/null && { echo \"$u is enabled\"; exit 1; }; done; exit 0", strings.Join(units, " "))
}

func notInstalled(bins ...string) string {
	return fmt.Sprintf("for b in %s; do command -v \"$b\" >/dev/null && { echo \"$b is installed\"; exit 1; }; done; exit 0", strings.Join(bins, " "))
}

// unsupportedControls returns the ids of controls with at least one result
// skipped because an InSpec resource does not support the target OS, plus
// 5.3.1, whose only_if gate makes it vanish with no results at all. It is
// waived today; once it is not, it needs a guest check here.
func unsupportedControls(reportPath string) []string {
	raw, err := os.ReadFile(reportPath)
	ExpectWithOffset(1, err).ToNot(HaveOccurred())
	var report struct {
		Profiles []struct {
			Controls []struct {
				ID      string `json:"id"`
				Results []struct {
					Status      string `json:"status"`
					SkipMessage string `json:"skip_message"`
				} `json:"results"`
			} `json:"controls"`
		} `json:"profiles"`
	}
	ExpectWithOffset(1, json.Unmarshal(raw, &report)).To(Succeed())
	ids := []string{}
	for _, p := range report.Profiles {
		for _, c := range p.Controls {
			if c.ID == "cis-dil-benchmark-5.3.1" && len(c.Results) == 0 {
				ids = append(ids, c.ID)
				continue
			}
			for _, r := range c.Results {
				if r.Status == "skipped" && strings.Contains(r.SkipMessage, "not supported on your OS") {
					ids = append(ids, c.ID)
					break
				}
			}
		}
	}
	return ids
}
