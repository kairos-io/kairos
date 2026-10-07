package mos_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
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

		// InSpec has no package or service support for Hadron, so the
		// controls in cisGuestChecks come back skipped, which exit 101 would
		// count as a pass. Each one is checked on the guest instead.
		guest := map[string]error{}
		By("checking the controls InSpec cannot evaluate on Hadron", func() {
			for id, check := range cisGuestChecks {
				out, gerr := vm.Sudo(check)
				if gerr != nil {
					gerr = fmt.Errorf("%s: %w\n%s", check, gerr, out)
				}
				guest[id] = gerr
			}
		})

		unchecked := []string{}
		for _, id := range unsupportedControls(reportPath) {
			if _, ok := cisGuestChecks[id]; !ok {
				unchecked = append(unchecked, id)
			}
		}

		// Written before any assertion so a failing run gets a summary too.
		writeCISStepSummary(reportPath, guest, unchecked)

		Expect(err).ToNot(HaveOccurred(),
			"CIS DIL L1 reported failures; failed controls are printed above; %s and %s are attached as artifacts",
			reportPath, cliLog)
		Expect(unchecked).To(BeEmpty(),
			"controls skipped as unsupported on this OS with no guest check in cisGuestChecks and no waiver")
		for id, gerr := range guest {
			Expect(gerr).ToNot(HaveOccurred(), "%s guest check failed", id)
		}
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

// writeCISStepSummary appends the CIS run to the GitHub Actions job summary
// when $GITHUB_STEP_SUMMARY is set, and does nothing otherwise. A report it
// cannot read is noted in the summary instead of failing the test here; the
// assertions after it report the real failure.
func writeCISStepSummary(reportPath string, guest map[string]error, unchecked []string) {
	summaryPath := os.Getenv("GITHUB_STEP_SUMMARY")
	if summaryPath == "" {
		return
	}
	f, err := os.OpenFile(summaryPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		GinkgoWriter.Printf("writeCISStepSummary: %v\n", err)
		return
	}
	defer f.Close()
	_, _ = f.WriteString(cisSummaryMarkdown(reportPath, guest, unchecked))
}

type cisControl struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	WaiverData struct {
		Justification      string `json:"justification"`
		SkippedDueToWaiver bool   `json:"skipped_due_to_waiver"`
	} `json:"waiver_data"`
	Results []struct {
		Status      string `json:"status"`
		CodeDesc    string `json:"code_desc"`
		SkipMessage string `json:"skip_message"`
	} `json:"results"`
}

// cisSummaryMarkdown renders the job summary: totals, failed controls, the
// guest checks, and the waivers with their reasons.
func cisSummaryMarkdown(reportPath string, guest map[string]error, unchecked []string) string {
	var b strings.Builder
	b.WriteString("## CIS Distribution Independent Linux v2.0.0, Level 1\n\n")

	raw, err := os.ReadFile(reportPath)
	var report struct {
		Profiles []struct {
			Name     string       `json:"name"`
			Version  string       `json:"version"`
			Controls []cisControl `json:"controls"`
		} `json:"profiles"`
	}
	if err == nil {
		err = json.Unmarshal(raw, &report)
	}
	if err != nil || len(report.Profiles) == 0 {
		fmt.Fprintf(&b, ":x: No usable cinc-auditor report at `%s` (%v). See the job log.\n\n", reportPath, err)
		return b.String()
	}

	var failed, waived []cisControl
	var passed, skipped int
	for _, c := range report.Profiles[0].Controls {
		if _, ok := cisGuestChecks[c.ID]; ok {
			continue // counted with the guest checks below
		}
		statuses := map[string]bool{}
		for _, r := range c.Results {
			statuses[r.Status] = true
		}
		switch {
		case c.WaiverData.SkippedDueToWaiver:
			waived = append(waived, c)
		case statuses["failed"]:
			failed = append(failed, c)
		case statuses["passed"]:
			passed++
		default:
			skipped++ // Level 2 only, or nothing on this system to check
		}
	}
	guestFailed := 0
	for _, gerr := range guest {
		if gerr != nil {
			guestFailed++
		}
	}

	if len(failed) == 0 && guestFailed == 0 && len(unchecked) == 0 {
		b.WriteString(":white_check_mark: All Level 1 controls pass or are waived.\n\n")
	} else {
		b.WriteString(":x: Some Level 1 controls fail.\n\n")
	}
	p := report.Profiles[0]
	fmt.Fprintf(&b, "Profile `%s` %s against an installed Hadron system.\n\n", p.Name, p.Version)
	b.WriteString("| Passed | Failed | Checked on the guest | Waived | Skipped (Level 2 or not applicable) |\n")
	b.WriteString("|---|---|---|---|---|\n")
	fmt.Fprintf(&b, "| %d | %d | %d/%d | %d | %d |\n\n", passed, len(failed), len(guest)-guestFailed, len(guest), len(waived), skipped)

	if len(failed) > 0 {
		b.WriteString("### Failed controls\n\n| Control | Title | First failing check |\n|---|---|---|\n")
		for _, c := range failed {
			first := ""
			for _, r := range c.Results {
				if r.Status == "failed" {
					first = r.CodeDesc
					break
				}
			}
			fmt.Fprintf(&b, "| %s | %s | %s |\n", cisShortID(c.ID), mdCell(c.Title), mdCell(first))
		}
		b.WriteString("\n")
	}

	if len(unchecked) > 0 {
		b.WriteString("### Skipped as unsupported, with no guest check\n\n")
		for _, id := range unchecked {
			fmt.Fprintf(&b, "- %s\n", cisShortID(id))
		}
		b.WriteString("\n")
	}

	ids := make([]string, 0, len(guest))
	for id := range guest {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return cisIDLess(ids[i], ids[j]) })
	b.WriteString("<details><summary>Checked on the guest (InSpec cannot evaluate these on Hadron)</summary>\n\n| Control | Result |\n|---|---|\n")
	for _, id := range ids {
		result := ":white_check_mark:"
		if guest[id] != nil {
			result = ":x: " + mdCell(guest[id].Error())
		}
		fmt.Fprintf(&b, "| %s | %s |\n", cisShortID(id), result)
	}
	b.WriteString("\n</details>\n\n")

	// The report carries no title for a control skipped by a waiver.
	fmt.Fprintf(&b, "<details><summary>Waived (%d)</summary>\n\n| Control | Why |\n|---|---|\n", len(waived))
	for _, c := range waived {
		fmt.Fprintf(&b, "| %s | %s |\n", cisShortID(c.ID), mdCell(c.WaiverData.Justification))
	}
	b.WriteString("\n</details>\n\n")
	return b.String()
}

// cisIDLess orders control ids by their numeric sections, so 2.2.3 comes
// before 2.2.10.
func cisIDLess(a, b string) bool {
	as := strings.Split(cisShortID(a), ".")
	bs := strings.Split(cisShortID(b), ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		x, _ := strconv.Atoi(as[i])
		y, _ := strconv.Atoi(bs[i])
		if x != y {
			return x < y
		}
	}
	return len(as) < len(bs)
}

func cisShortID(id string) string {
	return strings.TrimPrefix(id, "cis-dil-benchmark-")
}

// mdCell keeps a value on one table row.
func mdCell(s string) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\n", " ")
	return strings.ReplaceAll(s, "|", "\\|")
}
