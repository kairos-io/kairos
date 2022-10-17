package mos_test

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"time"

	. "github.com/spectrocloud/peg/matcher"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
)

var _ = Describe("kairos install test", Label("install-test"), func() {

	BeforeEach(func() {
		EventuallyConnects(1200)
	})

	AfterEach(func() {
		Machine.Clean()
		Machine.Create(context.Background())
		EventuallyConnects(1200)
	})

	testInstall := func(cloudConfig string, actual interface{}, m types.GomegaMatcher) {

		t, err := ioutil.TempFile("", "test")
		ExpectWithOffset(1, err).ToNot(HaveOccurred())

		defer os.RemoveAll(t.Name())
		err = ioutil.WriteFile(t.Name(), []byte(cloudConfig), os.ModePerm)
		Expect(err).ToNot(HaveOccurred())

		err = Machine.SendFile(t.Name(), "/tmp/config.yaml", "0770")
		Expect(err).ToNot(HaveOccurred())

		out, err := Sudo("sudo mv /tmp/config.yaml /oem/")
		Expect(err).ToNot(HaveOccurred(), out)

		out, err = Sudo("kairos-agent install")
		Expect(err).ToNot(HaveOccurred(), out)
		Expect(out).Should(ContainSubstring("Running after-install hook"))
		fmt.Println(out)
		Sudo("sync")

		detachAndReboot()

		EventuallyConnects(1200)
		Eventually(actual, 5*time.Minute, 10*time.Second).Should(m)
	}

	Context("install", func() {

		It("with bundles", func() {
			testInstall(`
install:
  auto: true
  device: /dev/sda
stages:
  initramfs:
  - name: "Set user and password"
    users:
     kairos:
      passwd: "kairos"
bundles:
- rootfs_path: "/usr/local/bin"
  targets:
  - container://quay.io/mocaccino/extra:edgevpn-utils-0.15.0
`, func() string {
				var out string
				out, _ = Sudo("/usr/local/bin/usr/bin/edgevpn --help")
				return out
			}, ContainSubstring("peerguard"))
		})
		It("cloud-config syntax mixed with extended syntax", func() {
			testInstall(`#cloud-config
install:
  auto: true
  device: /dev/sda
users:
- name: "kairos"
  passwd: "kairos"
stages:
  initramfs:
  - name: "Set user and password"
    commands:
    - echo "bar" > /etc/foo
`, func() string {
				var out string
				out, _ = Sudo("cat /etc/foo")
				return out
			}, ContainSubstring("bar"))
		})
		It("with config_url", func() {
			testInstall(`
install:
  auto: true
  device: /dev/sda
config_url: "https://gist.githubusercontent.com/mudler/6db795bad8f9e29ebec14b6ae331e5c0/raw/01137c458ad62cfcdfb201cae2f8814db702c6f9/testgist.yaml"`, func() string {
				var out string
				out, _ = Sudo("/usr/local/bin/usr/bin/edgevpn --help")
				return out
			}, ContainSubstring("peerguard"))
		})
	})
})
