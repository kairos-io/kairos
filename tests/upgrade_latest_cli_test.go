package mos_test

import (
	"fmt"
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/spectrocloud/peg/matcher"
)

var _ = Describe("k3s upgrade manual test", Label("upgrade-latest-with-cli"), func() {

	var vm VM
	containerImage := os.Getenv("CONTAINER_IMAGE")

	BeforeEach(func() {
		_, vm = startVM()
		vm.EventuallyConnects(1200)
	})

	AfterEach(func() {
		Expect(vm.Destroy(nil)).ToNot(HaveOccurred())
	})

	Context("upgrades", func() {
		BeforeEach(func() {
			expectDefaultService(vm)

			err := vm.Scp("assets/config.yaml", "/tmp/config.yaml", "0770")
			Expect(err).ToNot(HaveOccurred())

			out, err := vm.Sudo("/bin/bash -c 'set -o pipefail && kairos-agent manual-install --device auto /tmp/config.yaml 2>&1 | tee manual-install.txt'")
			Expect(err).ToNot(HaveOccurred(), out)

			Expect(out).Should(ContainSubstring("Running after-install hook"))
			vm.Sudo("sync")

			err = vm.DetachCD()
			Expect(err).ToNot(HaveOccurred())
			vm.Reboot()
		})

		It("can upgrade to current image", func() {
			currentVersion, err := vm.Sudo(". /etc/os-release; echo $VERSION")
			Expect(err).ToNot(HaveOccurred())
			Expect(currentVersion).To(ContainSubstring("v"))
			_, err = vm.Sudo("kairos-agent")
			if err == nil {
				out, err := vm.Sudo("kairos-agent upgrade --force --image " + containerImage)
				Expect(err).ToNot(HaveOccurred(), string(out))
				Expect(out).To(ContainSubstring("Upgrade completed"))
				Expect(out).To(ContainSubstring(containerImage))
				fmt.Println(out)
			} else {
				out, err := vm.Sudo("kairos upgrade --force --image " + containerImage)
				Expect(err).ToNot(HaveOccurred(), string(out))
				Expect(out).To(ContainSubstring("Upgrade completed"))
				Expect(out).To(ContainSubstring(containerImage))
			}

			vm.Reboot()

			Eventually(func() error {
				_, err := vm.Sudo(". /etc/os-release; echo $VERSION")
				return err
			}, 10*time.Minute, 10*time.Second).ShouldNot(HaveOccurred())

			var v string
			Eventually(func() string {
				v, _ = vm.Sudo(". /etc/os-release; echo $VERSION")
				return v
				// TODO: Add regex semver check here
			}, 30*time.Minute, 10*time.Second).ShouldNot(Equal(currentVersion))
		})
	})
})
