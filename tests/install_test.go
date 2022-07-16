package mos_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"time"

	"github.com/c3os-io/c3os/tests/machine"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
)

var _ = Describe("c3os install test", Label("install-test"), func() {

	BeforeEach(func() {
		machine.EventuallyConnects()
	})

	AfterEach(func() {
		if CurrentGinkgoTestDescription().Failed {
			gatherLogs()
		}
		machine.Delete()
		machine.Create(sshPort)
		machine.EventuallyConnects()
	})

	testInstall := func(cloudConfig string, actual interface{}, m types.GomegaMatcher) {

		t, err := ioutil.TempFile("", "test")
		ExpectWithOffset(1, err).ToNot(HaveOccurred())

		defer os.RemoveAll(t.Name())
		err = ioutil.WriteFile(t.Name(), []byte(cloudConfig), os.ModePerm)
		Expect(err).ToNot(HaveOccurred())

		err = machine.SendFile(t.Name(), "/tmp/config.yaml", "0770")
		Expect(err).ToNot(HaveOccurred())

		out, err := machine.Sudo("sudo mv /tmp/config.yaml /oem/")
		Expect(err).ToNot(HaveOccurred(), out)

		out, err = machine.Sudo("c3os-agent install")
		Expect(err).ToNot(HaveOccurred(), out)
		Expect(out).Should(ContainSubstring("Running after-install hook"))
		fmt.Println(out)
		machine.Sudo("sync")
		machine.DetachCD()
		machine.Restart()

		machine.EventuallyConnects()
		Eventually(actual, 5*time.Minute, 10*time.Second).Should(m)
	}

	Context("install", func() {

		It("with bundles", func() {
			testInstall(`
c3os:
  offline: true
  device: /dev/sda
stages:
  initramfs:
  - name: "Set user and password"
    users:
     c3os:
      passwd: "c3os"
bundles:
- rootfs_path: "/usr/local/bin"
  targets:
  - container://quay.io/mocaccino/extra:edgevpn-utils-0.15.0
`, func() string {
				var out string
				out, _ = machine.Sudo("/usr/local/bin/usr/bin/edgevpn --help")
				return out
			}, ContainSubstring("peerguard"))
		})
		It("with config_url", func() {
			testInstall(`
c3os:
  offline: true
  device: /dev/sda
config_url: "https://gist.githubusercontent.com/mudler/ab26e8dd65c69c32ab292685741ca09c/raw/ac4598da56c0cd912721d63457831424bb1d2f54/test.yaml"`, func() string {
				var out string
				out, _ = machine.Sudo("/usr/local/bin/usr/bin/edgevpn --help")
				return out
			}, ContainSubstring("peerguard"))
		})
	})
})
