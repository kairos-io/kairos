package mos_test

import (
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"time"

	"github.com/c3os-io/c3os/internal/utils"
	"github.com/c3os-io/c3os/tests/machine"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("c3os qr code install", Label("qrcode-install"), func() {
	BeforeEach(func() {
		machine.EventuallyConnects()
	})
	AfterEach(func() {
		if CurrentGinkgoTestDescription().Failed {
			gatherLogs()
		}
	})

	Context("live cd", func() {

		It("has default service active", func() {
			if os.Getenv("FLAVOR") == "alpine" {
				out, _ := machine.Sudo("rc-status")
				Expect(out).Should(ContainSubstring("c3os"))
				Expect(out).Should(ContainSubstring("c3os-agent"))
			} else {
				// Eventually(func() string {
				// 	out, _ := machine.SSHCommand("sudo systemctl status c3os-agent")
				// 	return out
				// }, 30*time.Second, 10*time.Second).Should(ContainSubstring("no network token"))

				out, _ := machine.Sudo("systemctl status c3os")
				Expect(out).Should(ContainSubstring("loaded (/etc/systemd/system/c3os.service; enabled; vendor preset: disabled)"))
			}
		})
	})

	Context("install", func() {
		It("to disk with custom config", func() {
			v, _ := machine.SSHCommand("cat /proc/cmdline")
			Expect(v).To(ContainSubstring("rd.cos.disable"))

			// sleep enough to give time to qr code to display.
			// TODO: This can be enhanced
			time.Sleep(2 * time.Minute)

			file, err := machine.Screenshot()
			Expect(err).ToNot(HaveOccurred())

			fmt.Println("Screenshot at ", file)

			defer os.RemoveAll(file)

			f2, err := ioutil.TempFile("", "fff")
			Expect(err).ToNot(HaveOccurred())

			resp, err := http.Get("https://github.com/mudler/edgevpn/releases/download/v0.15.3/edgevpn-v0.15.3-Darwin-x86_64.tar.gz")
			Expect(err).ToNot(HaveOccurred())

			defer resp.Body.Close()
			_, err = io.Copy(f2, resp.Body)
			Expect(err).ToNot(HaveOccurred())

			out, err := utils.SH("tar xvf " + f2.Name())
			fmt.Println(out)
			Expect(err).ToNot(HaveOccurred(), out)

			out, err = utils.SH(fmt.Sprintf("EDGEVPNTOKEN=%s ./edgevpn fs --name screenshot --path %s &", os.Getenv("EDGEVPNTOKEN"), file))
			fmt.Println(out)
			Expect(err).ToNot(HaveOccurred(), out)

			// Wait until we reboot into active, after the system is installed
			Eventually(func() string {
				v, _ = machine.SSHCommand("cat /proc/cmdline")
				return v
			}, 10*time.Minute, 10*time.Second).ShouldNot(ContainSubstring("rd.cos.disable"))
		})
	})
})
