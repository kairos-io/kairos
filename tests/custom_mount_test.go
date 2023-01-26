package mos_test

import (
	"context"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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

	Context("Install with custom mounts", func() {

		It("bind_mounts", func() {
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
`, func() []string {
				var out string
				out, _ = Sudo("cat /run/cos/cos-layout.env")
				return strings.Split(out, "\n")
			}, ContainElements(ContainSubstring("/mnt/bind1"), ContainSubstring("/mnt/ephemeral")), true)
		})

	})
})
