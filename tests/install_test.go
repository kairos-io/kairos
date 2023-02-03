package mos_test

import (
	"context"
	"os"
	"time"

	. "github.com/spectrocloud/peg/matcher"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
)

func testInstall(cloudConfig string, actual interface{}, m types.GomegaMatcher, should bool) {
	stateAssert("persistent.found", "false")

	t, err := os.CreateTemp("", "test")
	ExpectWithOffset(1, err).ToNot(HaveOccurred())

	defer os.RemoveAll(t.Name())
	err = os.WriteFile(t.Name(), []byte(cloudConfig), os.ModePerm)
	Expect(err).ToNot(HaveOccurred())

	err = Machine.SendFile(t.Name(), "/tmp/config.yaml", "0770")
	Expect(err).ToNot(HaveOccurred())

	out, err := Sudo(`kairos-agent manual-install --device "auto" /tmp/config.yaml`)
	Expect(err).ToNot(HaveOccurred(), out)
	Expect(out).Should(ContainSubstring("Running after-install hook"))
	Sudo("sync")

	detachAndReboot()

	EventuallyConnects(1200)
	if should {
		Eventually(actual, 5*time.Minute, 10*time.Second).Should(m)
	} else {
		Eventually(actual, 5*time.Minute, 10*time.Second).ShouldNot(m)
	}
}

func collectData() []string {
	results := []string{}
	cat, _ := Sudo("cat /etc/foo")
	layout, _ := Sudo("cat /run/cos/cos-layout.env")
	edgevpn, _ := Sudo("/usr/local/bin/usr/bin/edgevpn --help")
	results = append(results, cat, layout, edgevpn)
	return results
}

var _ = Describe("kairos install test", Label("install-test"), func() {

	BeforeEach(func() {
		EventuallyConnects(1200)
	})

	AfterEach(func() {
		Machine.Clean()
		Machine.Create(context.Background())
		EventuallyConnects(1200)
	})

	Context("install", func() {

		It("cloud-config syntax mixed with extended syntax", func() {
			testInstall(`#cloud-config
install:
  bind_mounts:
  - /mnt/bind1
  - /mnt/bind2
  ephemeral_mounts:
  - /mnt/ephemeral
  - /mnt/ephemeral2
users:
- name: "kairos"
  passwd: "kairos"
stages:
  initramfs:
  - name: "Set user and password"
    users:
      kairos:
         passwd: "kairos"
    commands:
    - echo "bar" > /etc/foo
bundles:
- rootfs_path: "/usr/local/bin"
  targets:
  - container://quay.io/mocaccino/extra:edgevpn-utils-0.15.0
`,
				collectData,
				ContainElements(ContainSubstring("bar"),
					ContainSubstring("/mnt/ephemeral"),
					ContainSubstring("/mnt/bind1"),
					ContainSubstring("peerguard")), true)
			stateAssert("persistent.found", "true")
		})

		It("with config_url", func() {
			testInstall(`
config_url: "https://gist.githubusercontent.com/mudler/6db795bad8f9e29ebec14b6ae331e5c0/raw/01137c458ad62cfcdfb201cae2f8814db702c6f9/testgist.yaml"`, func() string {
				var out string
				out, _ = Sudo("/usr/local/bin/usr/bin/edgevpn --help")
				return out
			}, ContainSubstring("peerguard"), true)
		})
	})
})
