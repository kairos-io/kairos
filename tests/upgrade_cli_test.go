package mos_test

import (
	"fmt"
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/spectrocloud/peg/matcher"
)

var _ = Describe("k3s upgrade manual test", Label("upgrade-with-cli"), func() {

	containerImage := os.Getenv("CONTAINER_IMAGE")

	BeforeEach(func() {
		EventuallyConnects(720)
	})

	Context("live cd", func() {

		It("has default service active", func() {
			if containerImage == "" {
				Fail("CONTAINER_IMAGE needs to be set")
			}

			if os.Getenv("FLAVOR") == "alpine" {
				out, _ := Sudo("rc-status")
				Expect(out).Should(ContainSubstring("kairos"))
				Expect(out).Should(ContainSubstring("kairos-agent"))
			} else {
				// Eventually(func() string {
				// 	out, _ := Machine.Command("sudo systemctl status kairos-agent")
				// 	return out
				// }, 30*time.Second, 10*time.Second).Should(ContainSubstring("no network token"))

				out, _ := Sudo("systemctl status kairos")
				Expect(out).Should(ContainSubstring("loaded (/etc/systemd/system/kairos.service; enabled"))
			}
		})
	})

	Context("install", func() {
		It("to disk with custom config", func() {
			err := Machine.SendFile("assets/config.yaml", "/tmp/config.yaml", "0770")
			Expect(err).ToNot(HaveOccurred())

			out, _ := Sudo("kairos-agent manual-install --device auto /tmp/config.yaml")
			Expect(out).Should(ContainSubstring("Running after-install hook"))
			fmt.Println(out)
			Sudo("sync")
			detachAndReboot()
		})
	})

	Context("upgrades", func() {
		It("can upgrade to current image", func() {

			currentVersion, err := Machine.Command("source /etc/os-release; echo $VERSION")
			Expect(err).ToNot(HaveOccurred())
			Expect(currentVersion).To(ContainSubstring("v"))
			_, err = Sudo("kairos-agent")
			if err == nil {
				out, err := Sudo("kairos-agent upgrade --force --image " + containerImage)
				Expect(err).ToNot(HaveOccurred(), string(out))
				Expect(out).To(ContainSubstring("Upgrade completed"))
				Expect(out).To(ContainSubstring(containerImage))
				fmt.Println(out)
			} else {
				out, err := Sudo("kairos upgrade --force --image " + containerImage)
				Expect(err).ToNot(HaveOccurred(), string(out))
				Expect(out).To(ContainSubstring("Upgrade completed"))
				Expect(out).To(ContainSubstring(containerImage))
				fmt.Println(out)
			}
			Reboot()

			Eventually(func() error {
				_, err := Machine.Command("source /etc/os-release; echo $VERSION")
				return err
			}, 10*time.Minute, 10*time.Second).ShouldNot(HaveOccurred())

			var v string
			Eventually(func() string {
				v, _ = Machine.Command("source /etc/os-release; echo $VERSION")
				return v
				// TODO: Add regex semver check here
			}, 10*time.Minute, 10*time.Second).Should(ContainSubstring("v"), v)
		})
	})
})
