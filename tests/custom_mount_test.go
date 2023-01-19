package mos_test

import (
	"context"
	"fmt"
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	. "github.com/spectrocloud/peg/matcher"
)

var _ = Describe("kairos test custom user mounts", Label("custom-mounts-test"), func() {

	BeforeEach(func() {
		EventuallyConnects(1200)
	})

	AfterEach(func() {
		if CurrentGinkgoTestDescription().Failed {
			gatherLogs()
		}
		Machine.Clean()
		Machine.Create(context.Background())
		EventuallyConnects(1200)

	})

	testInstall := func(cloudConfig string, actual interface{}, m types.GomegaMatcher, should bool) {
		stateAssert("persistent.found", "false")

		t, err := os.CreateTemp("", "test")
		ExpectWithOffset(1, err).ToNot(HaveOccurred())

		defer os.RemoveAll(t.Name())
		err = os.WriteFile(t.Name(), []byte(cloudConfig), os.ModePerm)
		Expect(err).ToNot(HaveOccurred())

		err = Machine.SendFile(t.Name(), "/tmp/config.yaml", "0770")
		Expect(err).ToNot(HaveOccurred())

		out, err := Sudo("sudo mv /tmp/config.yaml /oem/")
		Expect(err).ToNot(HaveOccurred(), out)

		out, _ = Sudo("cat /oem/config.yaml")
		fmt.Println(out)
		out, err = Sudo("kairos-agent install")
		Expect(err).ToNot(HaveOccurred(), out)
		Expect(out).Should(ContainSubstring("Running after-install hook"))
		fmt.Println(out)
		Sudo("sync")

		detachAndReboot()

		EventuallyConnects(1200)
		if should {
			fmt.Println("should ", should)
			Eventually(actual, 5*time.Minute, 10*time.Second).Should(m)
		} else {
			Eventually(actual, 5*time.Minute, 10*time.Second).ShouldNot(m)
		}

	}

	Context("Install with custom mounts", func() {

		It("does not have wrong key", func() {
			testInstall(`
install:
  auto: true
  device: "auto"
  bind_mounts:
  - /mnt/bind1
  - /mnt/bind2
  ephemeral_mounts:
  - /mnt/ephemeral
  - /mnt/ephemeral2
stages:
  initramfs:
  - name: "Set user and password"
    users:
     kairos:
      passwd: "kairos"
`, func() string {
				var out string
				out, _ = Sudo("cat /oem/90_custom.yaml")
				return out
			}, ContainSubstring("foo"), false)
		})
		FIt("bind_mounts", func() {
			testInstall(`
install:
  auto: true
  device: "auto"
  bind_mounts:
  - /mnt/bind1
  - /mnt/bind2
  ephemeral_mounts:
  - /mnt/ephemeral
  - /mnt/ephemeral2
stages:
  initramfs:
  - name: "Set user and password"
    users:
     kairos:
      passwd: "kairos"
`, func() string {
				var out string
				out, _ = Sudo("cat /run/cos/custom-layout.env")
				fmt.Println(out)
				return out
			}, ContainSubstring("/mnt/bind1 /mnt/bind2"), true)
		})

	})
})
