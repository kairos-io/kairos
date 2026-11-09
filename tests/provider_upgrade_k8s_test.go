// nolint
package mos_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/spectrocloud/peg/matcher"
)

var _ = Describe("k3s upgrade test", Label("provider", "provider-upgrade-k8s"), func() {
	var vm VM

	BeforeEach(func() {
		_, vm = startVM()
		vm.EventuallyConnects(1200)
	})

	AfterEach(func() {
		if CurrentGinkgoTestDescription().Failed {
			gatherLogs(vm)
		}
		vm.Destroy(nil)
	})

	It("installs to disk with custom config", func() {
		By("checking if it has default service active")

		expectDefaultService(vm)

		By("copy the config")
		err := vm.Scp("assets/single.yaml", "/tmp/config.yaml", "0770")
		Expect(err).ToNot(HaveOccurred())

		By("installing")
		out, err := vm.Sudo("kairos-agent manual-install /tmp/config.yaml")
		Expect(err).ToNot(HaveOccurred(), out)
		Expect(out).Should(ContainSubstring("Running after-install hook"))

		out, err = vm.Sudo("sync")
		Expect(err).ToNot(HaveOccurred(), out)

		By("rebooting after install")
		vm.Reboot()

		By("checking default services are on after first boot")
		if isFlavor(vm, "alpine") {
			Eventually(func() string {
				out, _ := vm.Sudo("rc-status")
				return out
			}, 30*time.Second, 10*time.Second).Should(And(
				ContainSubstring("kairos"),
				ContainSubstring("kairos-agent")))
			Eventually(func() string {
				var out string
				out, _ = vm.Sudo("rc-service kairos-agent status")
				return out
			}, 900*time.Second, 10*time.Second).Should(ContainSubstring("status: started"))
		} else {
			Eventually(func() string {
				out, _ := vm.Sudo("systemctl status kairos-agent")
				return out
			}, 30*time.Second, 10*time.Second).Should(ContainSubstring(
				"loaded (/etc/systemd/system/kairos-agent.service; enabled"))

			Eventually(func() string {
				out, _ := vm.Sudo("systemctl status systemd-timesyncd")
				return out
			}, 30*time.Second, 10*time.Second).Should(ContainSubstring(
				"loaded (/usr/lib/systemd/system/systemd-timesyncd.service; enabled"))
		}

		By("Checking agent provider correct start")
		Eventually(func() string {
			out, _ := vm.Sudo("journalctl -t kairos-provider")
			return out
		}, 900*time.Second, 10*time.Second).Should(Or(ContainSubstring("One time bootstrap starting"), ContainSubstring("Sentinel exists")))

		By("Checking k3s is pointing to https")
		Eventually(func() string {
			out, _ := vm.Sudo("cat /etc/rancher/k3s/k3s.yaml")
			return out
		}, 900*time.Second, 10*time.Second).Should(ContainSubstring("https:"))

		/*
			By("checking if logs are rotated")
			out, err = vm.Sudo("logrotate -vf /etc/logrotate.d/kairos")
			Expect(err).ToNot(HaveOccurred())
			Expect(out).To(ContainSubstring("log needs rotating"))
			// Check that we have some rotated logs
			out, err = vm.Sudo("[ $(ls /var/log/kairos/agent-*log.1.gz 2>/dev/null | wc -l) -gt 0 ]")
			Expect(err).ToNot(HaveOccurred(), out)
		*/

		By("checking current version before upgrade")
		currentVersion, err := vm.Sudo(getVersionCmd)
		Expect(err).ToNot(HaveOccurred())
		Expect(currentVersion).To(ContainSubstring("v"))
		fmt.Printf("Current version before upgrade: %s\n", currentVersion)

		deployKairosOperator(vm)

		// Opportunistic feature test: verify we can find a matching release image
		// in quay.io/kairos/hadron for the current variant/arch/model
		By("verifying matching container images exist in quay.io/kairos/hadron")

		// Get current image parameters from kairos-release
		variant, err := vm.Sudo(`. /etc/kairos-release && echo -n $KAIROS_VARIANT`)
		Expect(err).ToNot(HaveOccurred(), "failed to get KAIROS_VARIANT")
		variant = strings.TrimSpace(variant)

		arch, err := vm.Sudo(`. /etc/kairos-release && echo -n $KAIROS_TARGETARCH`)
		Expect(err).ToNot(HaveOccurred(), "failed to get KAIROS_TARGETARCH")
		arch = strings.TrimSpace(arch)

		model, err := vm.Sudo(`. /etc/kairos-release && echo -n $KAIROS_MODEL`)
		Expect(err).ToNot(HaveOccurred(), "failed to get KAIROS_MODEL")
		model = strings.TrimSpace(model)

		fmt.Printf("Looking for images matching: variant=%s, arch=%s, model=%s\n", variant, arch, model)

		// Query quay.io/kairos/hadron for tags matching variant-arch-model pattern
		// Tags look like: v0.0.4-standard-amd64-generic-v4.0.3-k3sv1.33.9-k3s1
		// Note: quay.io API requires "like:" prefix for pattern matching
		filterPattern := fmt.Sprintf("%s-%s-%s", variant, arch, model)
		checkCmd := fmt.Sprintf(`curl -sf "https://quay.io/api/v1/repository/kairos/hadron/tag/?filter_tag_name=like:%s&limit=1" | grep -q '"name"'`, filterPattern)
		_, err = vm.Sudo(checkCmd)
		Expect(err).ToNot(HaveOccurred(), "expected to find container images matching %s in quay.io/kairos/hadron", filterPattern)

		By("finding the upgrade image")
		upgradeImage, err := getUpgradeImage(vm)
		Expect(err).ToNot(HaveOccurred(), "failed to get upgrade image")
		fmt.Printf("Using upgrade image: %s\n", upgradeImage)

		expectOperatorRefusesDowngrade(vm, upgradeImage)

		By("preparing the NodeOpUpgrade resource")

		tempDir, err := os.MkdirTemp("", "nodeopupgrade-*")
		Expect(err).ToNot(HaveOccurred())
		defer os.RemoveAll(tempDir)

		b, err := os.ReadFile("assets/nodeopupgrade.yaml")
		Expect(err).ToNot(HaveOccurred())

		nodeOpUpgrade := fmt.Sprintf(string(b), upgradeImage)
		err = os.WriteFile(filepath.Join(tempDir, "nodeopupgrade.yaml"), []byte(nodeOpUpgrade), 0777)
		Expect(err).ToNot(HaveOccurred())

		err = vm.Scp(filepath.Join(tempDir, "nodeopupgrade.yaml"), "./nodeopupgrade.yaml", "0770")
		Expect(err).ToNot(HaveOccurred())

		By("applying the NodeOpUpgrade resource")
		Eventually(func() string {
			out, _ := kubectl(vm, "apply -f nodeopupgrade.yaml")
			return out
		}, 900*time.Second, 10*time.Second).Should(Or(ContainSubstring("created"), ContainSubstring("unchanged")))

		By("checking that the upgrade job is created")
		Eventually(func() string {
			out, _ = kubectl(vm, "get pods -n operator-system")
			return out
		}, 900*time.Second, 10*time.Second).Should(ContainSubstring("kairos-upgrade"))

		By("waiting for the upgrade to complete")
		Eventually(func() string {
			out, _ = kubectl(vm, "get nodeopupgrade -n operator-system kairos-upgrade -o jsonpath='{.status.phase}'")
			return out
		}, 30*time.Minute, 10*time.Second).Should(ContainSubstring("Completed"))

		By("validate node is running after upgrade")
		// After a successful upgrade and reboot, verify the node is healthy
		// Note: When upgrading to the same version (e.g., CI builds from same PR),
		// the version string might not change, so we verify system health instead
		Eventually(func() string {
			newVersion, _ := vm.Sudo(getVersionCmd)
			fmt.Printf("version after upgrade = %+v\n", newVersion)
			return newVersion
		}, 30*time.Minute, 10*time.Second).Should(ContainSubstring("v"), func() string {
			debugOutput := "DEBUG OUTPUT\n--------------------------------\n"
			debugOutput += fmt.Sprintf("version before upgrade: %s\n", currentVersion)
			debugOutput += fmt.Sprintf("upgrade image used: %s\n", upgradeImage)

			nodeOpStatus, _ := kubectl(vm, "get nodeopupgrade -n operator-system kairos-upgrade -o yaml")
			debugOutput += fmt.Sprintf("nodeopupgrade status:\n%s\n", nodeOpStatus)

			pods, _ := kubectl(vm, "get pods -A")
			debugOutput += fmt.Sprintf("pods:\n%s\n", pods)

			return debugOutput
		})

		// Verify k3s is still operational after the upgrade
		By("verifying k3s is operational after upgrade")
		Eventually(func() string {
			out, _ := kubectl(vm, "get nodes")
			return out
		}, 5*time.Minute, 10*time.Second).Should(ContainSubstring("Ready"))
	})
})

// expectOperatorRefusesDowngrade checks that an upgrade through kairos-operator
// is refused when the target image was built with an older kairos-init than the
// node (kairos-io/kairos#6033). The upgrade pod runs the target image, so its
// own /etc/kairos-release is the target's, and the agent has to read the
// node's copy under HOST_DIR instead. Raising KAIROS_INIT_VERSION in the node's
// /etc/kairos-release, which lasts until the next reboot, turns an upgrade to
// the image under test into a downgrade without building an older image.
func expectOperatorRefusesDowngrade(vm VM, upgradeImage string) {
	By("making the node look newer than the upgrade image")
	out, err := vm.Sudo("cp /etc/kairos-release /tmp/kairos-release.orig")
	Expect(err).ToNot(HaveOccurred(), out)
	out, err = vm.Sudo(`sed -i 's/^KAIROS_INIT_VERSION=.*/KAIROS_INIT_VERSION="v99.0.0"/' /etc/kairos-release`)
	Expect(err).ToNot(HaveOccurred(), out)
	out, err = vm.Sudo("grep KAIROS_INIT_VERSION /etc/kairos-release")
	Expect(err).ToNot(HaveOccurred(), out)
	Expect(out).To(ContainSubstring("v99.0.0"))

	bootID, err := vm.Sudo("cat /proc/sys/kernel/random/boot_id")
	Expect(err).ToNot(HaveOccurred(), bootID)

	By("applying a NodeOpUpgrade that is a downgrade for this node")
	nodeOpUpgrade := fmt.Sprintf(`apiVersion: operator.kairos.io/v1alpha1
kind: NodeOpUpgrade
metadata:
  name: kairos-downgrade
  namespace: operator-system
spec:
  image: %q
  concurrency: 1
  stopOnFailure: true
  # The node already runs this image, so without force the operator skips
  # it as up to date and the agent never runs.
  force: true
`, upgradeImage)
	tempDir, err := os.MkdirTemp("", "nodeopupgrade-*")
	Expect(err).ToNot(HaveOccurred())
	defer os.RemoveAll(tempDir)
	Expect(os.WriteFile(filepath.Join(tempDir, "downgrade.yaml"), []byte(nodeOpUpgrade), 0644)).To(Succeed())
	Expect(vm.Scp(filepath.Join(tempDir, "downgrade.yaml"), "./downgrade.yaml", "0770")).To(Succeed())
	Eventually(func() string {
		out, _ := kubectl(vm, "apply -f downgrade.yaml")
		return out
	}, 900*time.Second, 10*time.Second).Should(Or(ContainSubstring("created"), ContainSubstring("unchanged")))

	By("waiting for the upgrade pod to refuse the downgrade")
	// The job retries a failed pod, so wait for the refusal in the logs of
	// any attempt rather than for the NodeOpUpgrade to reach Failed.
	logsCmd := `sh -c 'for p in $(k3s kubectl get pods -n operator-system -o name | grep kairos-downgrade); do k3s kubectl logs -n operator-system --all-containers "$p"; done'`
	Eventually(func() string {
		out, _ := vm.Sudo(logsCmd)
		if !strings.Contains(out, "refusing to upgrade") {
			phase, _ := kubectl(vm, "get nodeopupgrade -n operator-system kairos-downgrade -o jsonpath='{.status.phase}'")
			if strings.Contains(phase, "Completed") {
				StopTrying("the downgrade NodeOpUpgrade completed instead of being refused").Now()
			}
		}
		return out
	}, 20*time.Minute, 10*time.Second).Should(ContainSubstring("refusing to upgrade"), func() string {
		out, _ := kubectl(vm, "get nodeopupgrade,nodeop,jobs,pods -n operator-system -o wide")
		return out
	})

	out, _ = vm.Sudo(logsCmd)
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "KAIROS_INIT_VERSION") || strings.Contains(line, "kairos-agent") {
			fmt.Println(line)
		}
	}

	By("removing the refused NodeOpUpgrade and restoring the node")
	out, err = kubectl(vm, "delete nodeopupgrade -n operator-system kairos-downgrade --wait=true")
	Expect(err).ToNot(HaveOccurred(), out)
	Eventually(func() string {
		out, _ := kubectl(vm, "get pods -n operator-system")
		return out
	}, 5*time.Minute, 5*time.Second).ShouldNot(ContainSubstring("kairos-downgrade"))
	out, err = vm.Sudo("cp /tmp/kairos-release.orig /etc/kairos-release")
	Expect(err).ToNot(HaveOccurred(), out)
	out, err = vm.Sudo(`sh -c 'k3s kubectl uncordon $(k3s kubectl get nodes -o name)'`)
	Expect(err).ToNot(HaveOccurred(), out)

	By("checking the node was not rebooted by the refused upgrade")
	nowBootID, err := vm.Sudo("cat /proc/sys/kernel/random/boot_id")
	Expect(err).ToNot(HaveOccurred(), nowBootID)
	Expect(nowBootID).To(Equal(bootID))
}
