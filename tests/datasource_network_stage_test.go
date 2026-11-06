package mos_test

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/spectrocloud/peg/matcher"
)

// The guest reaches the host as 10.0.2.2 over QEMU user networking.
const datasourceHostAddr = "10.0.2.2"

const datasourceUser = "dsuser"

const datasourceUserKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGgg3iiwC7m8iO6PqNyYmyaqj6dFQv77r3ES4+wZUTmG datasource-network-test"

// Each marker records the cgroup of the process that ran the stage. The
// catch-up in 00_datasource.yaml runs from the network stage, so a marker
// written by it names cos-setup-network.service.
const (
	datasourceInitramfsMarker = "/run/datasource-userdata-initramfs"
	datasourceBootMarker      = "/run/datasource-userdata-boot"
)

// datasourceUserdata is what the fake MAAS metadata service hands out as
// user-data. The users entry lands in the boot stage, through yip's
// cloud-init loader.
var datasourceUserdata = fmt.Sprintf(`#cloud-config
users:
- name: %s
  passwd: %s
  ssh_authorized_keys:
  - %s
stages:
  initramfs:
  - name: "Record who ran the userdata initramfs stage"
    commands:
    - cat /proc/self/cgroup >> %s
  boot:
  - name: "Record who ran the userdata boot stage"
    commands:
    - cat /proc/self/cgroup >> %s
`, datasourceUser, datasourceUser, datasourceUserKey, datasourceInitramfsMarker, datasourceBootMarker)

// fakeMAASServer serves the preseed that cloud-config-url points at and the
// metadata the yip MAAS provider asks for. It serves no public-keys, so the
// only SSH key the machine can get is the one in the userdata.
func fakeMAASServer(port int) http.Handler {
	base := fmt.Sprintf("http://%s:%d/maas", datasourceHostAddr, port)
	preseed := fmt.Sprintf(`datasource:
  MAAS:
    metadata_url: %s
    consumer_key: test
    token_key: test
    token_secret: test
`, base)

	mux := http.NewServeMux()
	mux.HandleFunc("/preseed", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, preseed)
	})
	mux.HandleFunc("/maas/2012-03-01/meta-data/local-hostname", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "datasource-network")
	})
	mux.HandleFunc("/maas/2012-03-01/meta-data/instance-id", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "datasource-network-test")
	})
	mux.HandleFunc("/maas/2012-03-01/user-data", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, datasourceUserdata)
	})
	return mux
}

// Userdata that a datasource can only fetch once the network is up must still
// apply its initramfs and boot stages on that same boot. 00_datasource.yaml
// catches up on them from the network stage. The machine is installed from
// the live media over SSH, without a cidata CD, because a CD datasource is
// found in the initramfs and never reaches the network stage.
var _ = Describe("datasource userdata fetched in the network stage", Label("datasource-network-stage"), func() {
	var vm VM

	BeforeEach(func() {
		ln, err := net.Listen("tcp", "0.0.0.0:0")
		Expect(err).ToNot(HaveOccurred())
		port := ln.Addr().(*net.TCPAddr).Port
		srv := &http.Server{Handler: fakeMAASServer(port)}
		go func() { _ = srv.Serve(ln) }()
		DeferCleanup(srv.Close)

		Expect(os.Unsetenv("DATASOURCE")).To(Succeed())
		_, vm = startVM()
		vm.EventuallyConnects(1200)

		config := fmt.Sprintf(`#cloud-config
install:
  device: /dev/vda
  grub_options:
    extra_cmdline: "cloud-config-url=http://%s:%d/preseed"
users:
- name: kairos
  passwd: kairos
  groups:
  - admin
`, datasourceHostAddr, port)
		configPath := filepath.Join(vm.StateDir, "install-config.yaml")
		Expect(os.WriteFile(configPath, []byte(config), 0o600)).To(Succeed())
		Expect(vm.Scp(configPath, "/tmp/config.yaml", "0770")).To(Succeed())
	})

	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			gatherLogs(vm)
		}
		Expect(vm.Destroy(nil)).To(Succeed())
	})

	It("applies the userdata initramfs and boot stages on the first boot", func() {
		if isFlavor(vm, "alpine") {
			Skip("Alpine brings the network up in the initramfs, so the datasource is fetched before the network stage")
		}

		out, err := vm.Sudo("kairos-agent manual-install --device /dev/vda /tmp/config.yaml")
		Expect(err).ToNot(HaveOccurred(), out)
		Expect(out).To(ContainSubstring("Running after-install hook"))

		// The only reboot in this spec. Everything below is checked on the
		// first boot of the installed system.
		vm.Reboot(1200)
		Eventually(func() string {
			out, _ := vm.Sudo("kairos-agent state get boot")
			return out
		}, 10*time.Minute, 10*time.Second).Should(ContainSubstring("active_boot"))

		out, err = vm.Sudo("cat /proc/cmdline")
		Expect(err).ToNot(HaveOccurred(), out)
		Expect(out).To(ContainSubstring("cloud-config-url="))

		By("waiting for the network stage to finish", func() {
			Eventually(func() string {
				out, _ := vm.Sudo("systemctl show -p ActiveState --value cos-setup-network")
				return strings.TrimSpace(out)
			}, 5*time.Minute, 5*time.Second).Should(Equal("active"))
		})

		By("checking that the network stage ran the userdata initramfs stage", func() {
			out, err := vm.Sudo("cat " + datasourceInitramfsMarker)
			Expect(err).ToNot(HaveOccurred(), "the userdata initramfs stage did not run on the first boot: %s", out)
			Expect(out).To(ContainSubstring("cos-setup-network.service"),
				"the userdata initramfs stage ran, but not from the network stage, so the datasource was found before it: %s", out)
		})

		By("checking that the network stage ran the userdata boot stage", func() {
			out, err := vm.Sudo("cat " + datasourceBootMarker)
			Expect(err).ToNot(HaveOccurred(), "the userdata boot stage did not run on the first boot: %s", out)
			Expect(out).To(ContainSubstring("cos-setup-network.service"), out)
		})

		By("checking the user and SSH key from the userdata", func() {
			out, err := vm.Sudo("getent passwd " + datasourceUser)
			Expect(err).ToNot(HaveOccurred(), out)
			Expect(out).To(HavePrefix(datasourceUser + ":"))

			out, err = vm.Sudo(fmt.Sprintf("cat ~%s/.ssh/authorized_keys", datasourceUser))
			Expect(err).ToNot(HaveOccurred(), out)
			Expect(out).To(ContainSubstring(datasourceUserKey))
		})
	})
})
