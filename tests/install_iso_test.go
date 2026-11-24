package mos_test

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	. "github.com/spectrocloud/peg/matcher"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// isoServer serves the ISO the suite booted from over plain HTTP, so the
// guest can install from it as if it were a remote image.
type isoServer struct {
	server *httptest.Server
	port   int
	hits   atomic.Int64
}

// startISOServer binds on all interfaces on a free port. httptest's own
// listener is loopback-only and the guest arrives through the slirp gateway.
func startISOServer(isoPath string) *isoServer {
	GinkgoHelper()

	listener, err := net.Listen("tcp", "0.0.0.0:0")
	Expect(err).ToNot(HaveOccurred())

	s := &isoServer{port: listener.Addr().(*net.TCPAddr).Port}
	s.server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.hits.Add(1)
		http.ServeFile(w, r, isoPath)
	}))
	_ = s.server.Listener.Close()
	s.server.Listener = listener
	s.server.Start()

	return s
}

// URL is the ISO address as seen from inside the guest, through the QEMU
// user-net gateway.
func (s *isoServer) URL() string {
	return fmt.Sprintf("http://%s:%d/install.iso", configURLGuestHost, s.port)
}

// Hits is the number of ISO requests served so far.
func (s *isoServer) Hits() int64 {
	return s.hits.Load()
}

func (s *isoServer) Close() {
	s.server.Close()
}

var _ = Describe("kairos install from install.iso", Label("install-iso"), func() {
	var vm VM
	var server *isoServer

	BeforeEach(func() {
		_, vm = startVM()
		vm.EventuallyConnects(1200)
	})

	AfterEach(func() {
		if server != nil {
			server.Close()
			server = nil
		}

		if CurrentSpecReport().Failed() {
			serial, _ := os.ReadFile(filepath.Join(vm.StateDir, "serial.log"))
			_ = os.MkdirAll("logs", os.ModePerm|os.ModeDir)
			_ = os.WriteFile(filepath.Join("logs", "serial.log"), serial, os.ModePerm)
			fmt.Println(string(serial))

			gatherLogs(vm)
		}
		Expect(vm.Destroy(nil)).ToNot(HaveOccurred())
	})

	It("installs from the ISO served over HTTP and boots the installed system", func() {
		expectSecureBootEnabled(vm)

		server = startISOServer(os.Getenv("ISO"))

		out := testInstall(fmt.Sprintf(`#cloud-config
install:
  iso: "%s"
users:
- name: "kairos"
  passwd: "kairos"
  groups:
    - "admin"
`, server.URL()), vm)

		By("checking the agent mounted the downloaded ISO", func() {
			Expect(out).To(ContainSubstring("Mounting iso"))
			// Separates "the agent installed from the downloaded ISO" from
			// "the agent installed from the live media".
			Expect(server.Hits()).To(BeNumerically(">", 0),
				"the guest never fetched the ISO from the test server")
		})

		By("checking the installed system booted from disk", func() {
			expectSecureBootEnabled(vm)

			Eventually(func() string {
				out, err := vm.Sudo("kairos-agent state get boot")
				if err != nil {
					return ""
				}
				return out
			}, 5*time.Minute, 10*time.Second).Should(ContainSubstring("active_boot"))

			stateAssertVM(vm, "persistent.found", "true")
			stateAssertVM(vm, "oem.mounted", "true")
			stateAssertVM(vm, "state.mounted", "true")
		})
	})
})
