package mos_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/spectrocloud/peg/matcher"
)

// The diff-upgrade suite installs a node, builds two synthetic releases on
// top of the installed one (see assets/diff-upgrade-releases.sh), upgrades
// through both with diffs only, and then runs the acceptance checks on the
// result.
//
// Every diff upgrade is given a full image source that does not exist, so
// a diff that silently fell back to the full image fails the upgrade
// instead of passing it.
var _ = Describe("kairos diff upgrade test", Label("diff-upgrade"), func() {
	const (
		work        = "/usr/local/diff-test"
		testDir     = "/usr/share/kairos-diff-test"
		missingFull = "dir:/nonexistent-full-image"
	)
	var vm VM
	var datasource string

	BeforeEach(func() {
		datasource = CreateDatasource("assets/autoinstall.yaml")
		Expect(os.Setenv("DATASOURCE", datasource)).ToNot(HaveOccurred())
		_, vm = startVM()
		vm.EventuallyConnects(1200)
	})

	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			gatherLogs(vm)
			serial, _ := os.ReadFile(filepath.Join(vm.StateDir, "serial.log"))
			_ = os.MkdirAll("logs", os.ModePerm|os.ModeDir)
			_ = os.WriteFile(filepath.Join("logs", "serial.log"), serial, os.ModePerm)
			fmt.Println(string(serial))
		}

		Expect(vm.Destroy(nil)).To(Succeed())
		Expect(os.Unsetenv("DATASOURCE")).To(Succeed())
		Expect(os.Remove(datasource)).To(Succeed())
	})

	diffUpgrade := func(diff string) string {
		out, err := vm.Sudo(fmt.Sprintf("kairos-agent --debug upgrade --source %s --diff-source ocifile:%s/%s 2>&1", missingFull, work, diff))
		Expect(err).ToNot(HaveOccurred(), out)
		Expect(out).To(ContainSubstring("Applying diff"))
		Expect(out).ToNot(ContainSubstring("falling back to the full image"))
		Expect(out).To(ContainSubstring("Upgrade completed"))
		return out
	}

	rebootToRelease := func(release string) {
		vm.Reboot()
		Eventually(func() string {
			out, _ := vm.Sudo("grep KAIROS_DIFF_TEST /etc/kairos-release")
			return out
		}, 10*time.Minute, 10*time.Second).Should(ContainSubstring(fmt.Sprintf(`KAIROS_DIFF_TEST="%s"`, release)))
		out, err := vm.Sudo("kairos-agent state get boot")
		Expect(err).ToNot(HaveOccurred(), out)
		Expect(out).To(ContainSubstring("active_boot"))
	}

	expectFile := func(path, content string) {
		out, err := vm.Sudo("cat " + path)
		ExpectWithOffset(1, err).ToNot(HaveOccurred(), out)
		ExpectWithOffset(1, strings.TrimSpace(out)).To(Equal(content))
	}

	expectNoFile := func(path string) {
		_, err := vm.Sudo("test -e " + path)
		ExpectWithOffset(1, err).To(HaveOccurred(), path+" should not exist")
	}

	It("upgrades through two diffs and still passes the acceptance checks", func() {
		expectDefaultService(vm)
		expectStartedInstallation(vm)
		expectRebootedToActive(vm)

		By("building releases 2 and 3 and the diffs between them", func() {
			Expect(vm.Scp("assets/diff-upgrade-releases.sh", "/tmp/diff-upgrade-releases.sh", "0755")).To(Succeed())
			out, err := vm.Sudo("sh /tmp/diff-upgrade-releases.sh 2>&1")
			Expect(err).ToNot(HaveOccurred(), out)
			reportDiffSizes(out)
		})

		By("upgrading from release 1 to 2 with a diff", func() {
			diffUpgrade("d12.tar")
			rebootToRelease("2")
			expectFile(testDir+"/release", "two")
			expectFile(testDir+"/only-in-2", "only-in-2")
		})

		By("upgrading from release 2 to 3 with a diff", func() {
			diffUpgrade("d23.tar")
			rebootToRelease("3")
			expectFile(testDir+"/release", "three")
			expectFile(testDir+"/only-in-3", "only-in-3")
			expectNoFile(testDir + "/only-in-2")
			expectNoFile(testDir + "/blob-2")
		})

		By("refusing a diff made for another release and leaving the system alone", func() {
			// d12 applies to release 1, the node runs release 3. The
			// upgrade falls back to the full image, which does not exist,
			// so it fails before active or passive are touched.
			out, err := vm.Sudo(fmt.Sprintf("kairos-agent --debug upgrade --source %s --diff-source ocifile:%s/d12.tar 2>&1", missingFull, work))
			Expect(err).To(HaveOccurred(), out)
			Expect(out).To(ContainSubstring("falling back to the full image"))
			Expect(out).To(ContainSubstring("diff base does not match"))
			expectNoFile("/run/initramfs/cos-state/cOS/transition.img")
			rebootToRelease("3")
		})

		By("passing the acceptance checks on the upgraded node", func() {
			expectAcceptance(vm)
		})
	})
})

// reportDiffSizes prints the sizes assets/diff-upgrade-releases.sh reported,
// against the full release, to the test log and to the job summary when
// running under GitHub Actions.
func reportDiffSizes(out string) {
	sizes := map[string]int64{}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) != 3 || fields[0] != "SIZE" {
			continue
		}
		n, err := strconv.ParseInt(fields[2], 10, 64)
		Expect(err).ToNot(HaveOccurred(), line)
		sizes[fields[1]] = n
	}
	Expect(sizes).To(HaveKey("full-2"), out)
	full := sizes["full-2"]

	var b strings.Builder
	b.WriteString("### Diff upgrade sizes\n\n")
	b.WriteString("| Archive | Size | Of the full release |\n|---|---|---|\n")
	for _, row := range []struct{ name, label string }{
		{"full-2", "Full release 2"},
		{"d12", "Diff 1 to 2"},
		{"d23", "Diff 2 to 3"},
		{"d13", "Diff 1 to 3"},
	} {
		Expect(sizes).To(HaveKey(row.name), out)
		fmt.Fprintf(&b, "| %s | %.1f MiB | %.1f%% |\n", row.label, float64(sizes[row.name])/(1<<20), 100*float64(sizes[row.name])/float64(full))
	}
	GinkgoWriter.Println(b.String())
	fmt.Println(b.String())

	if summary := os.Getenv("GITHUB_STEP_SUMMARY"); summary != "" {
		f, err := os.OpenFile(summary, os.O_APPEND|os.O_WRONLY, 0o644)
		if err == nil {
			_, _ = f.WriteString(b.String())
			_ = f.Close()
		}
	}
}
