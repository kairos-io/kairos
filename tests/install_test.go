package mos_test

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	. "github.com/spectrocloud/peg/matcher"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var stateAssertVM = func(vm VM, query, expected string) {
	out, err := vm.Sudo(fmt.Sprintf("kairos-agent state get %s", query))
	ExpectWithOffset(1, err).ToNot(HaveOccurred(), out)
	ExpectWithOffset(1, out).To(ContainSubstring(expected))
}

func testInstall(cloudConfig string, vm VM) string { //, actual interface{}, m types.GomegaMatcher) {
	stateAssertVM(vm, "persistent.found", "false")

	t, err := os.CreateTemp("", "test")
	ExpectWithOffset(1, err).ToNot(HaveOccurred())

	defer os.RemoveAll(t.Name())
	err = os.WriteFile(t.Name(), []byte(cloudConfig), os.ModePerm)
	Expect(err).ToNot(HaveOccurred())

	err = vm.Scp(t.Name(), "/tmp/config.yaml", "0770")
	Expect(err).ToNot(HaveOccurred())

	var out string
	By("installing kairos", func() {
		out, err = vm.Sudo(`kairos-agent --debug manual-install --device "auto" /tmp/config.yaml`)
		Expect(err).ToNot(HaveOccurred(), out)
		Expect(out).Should(ContainSubstring("Running after-install hook"))
		vm.Sudo("sync")
	})

	By("waiting for VM to reboot", func() {
		vm.Reboot()
		vm.EventuallyConnects(1200)
	})

	return out
}

// configURLMarkerPath is written only by the payload startConfigURLServer
// serves. Nothing in the base image, the ISO, or the inline cloud-config of
// the config_url cells touches it, so its presence is evidence that the
// remote payload was fetched, merged and run, and its absence is evidence
// that it was not.
const configURLMarkerPath = "/run/kairos-config-url-applied"

// configURLMarkerContent is the marker body. A fixed string rather than a
// bare file so a stray empty file cannot pass the assertion.
const configURLMarkerContent = "config-url-payload-applied"

// configURLGuestHost is the QEMU user-net (slirp) gateway. The guest reaches
// services listening on the test host through it, the same way the
// insecure-registry cell reaches the registry container.
const configURLGuestHost = "10.0.2.2"

// configURLPayload is the remote cloud-config the reachable cell points at.
//
// The #cloud-config header is load-bearing: sdk/collector.fetchRemoteConfig
// runs HasValidHeader over the response body and, if it does not match,
// discards the payload and returns an empty config with a nil error. A
// header-less remote config is therefore a silent no-op. The gist this cell
// used to point at (Itxaka/c94e42bd52a67e2c9bffd11b8e633e38) starts at
// "stages:" with no header, so nothing it declared was ever applied and the
// old "boot: active_boot" assertion could not tell.
//
// The stage is "network" on purpose. cos-setup-network.service is
// After=network-online.target, so it is the first stage guaranteed to run
// with the network up. The earlier "fs" stage runs Before=sysinit.target with
// no network dependency at all and its fetch can fail quietly, which would
// make this cell flaky rather than meaningful.
var configURLPayload = fmt.Sprintf(`#cloud-config
stages:
  network:
    - name: "config_url payload marker"
      commands:
        - echo %s > %s
`, configURLMarkerContent, configURLMarkerPath)

// configURLServer serves configURLPayload to the guest over plain HTTP and
// counts the requests it answered.
type configURLServer struct {
	server *httptest.Server
	port   int
	hits   atomic.Int64
}

// startConfigURLServer binds a payload server on a free port on all
// interfaces. httptest's own listener is loopback-only; the guest arrives
// through the slirp gateway, so the listener has to accept on 0.0.0.0.
// Fails the current spec if the listener cannot be opened.
func startConfigURLServer() *configURLServer {
	GinkgoHelper()

	listener, err := net.Listen("tcp", "0.0.0.0:0")
	Expect(err).ToNot(HaveOccurred())

	c := &configURLServer{port: listener.Addr().(*net.TCPAddr).Port}
	c.server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		c.hits.Add(1)
		w.Header().Set("Content-Type", "text/yaml")
		_, _ = w.Write([]byte(configURLPayload))
	}))
	_ = c.server.Listener.Close()
	c.server.Listener = listener
	c.server.Start()

	return c
}

// URL is the address to put in config_url, as seen from inside the guest.
func (c *configURLServer) URL() string {
	return fmt.Sprintf("http://%s:%d/payload.yaml", configURLGuestHost, c.port)
}

// Hits is the number of payload requests served so far.
func (c *configURLServer) Hits() int64 {
	return c.hits.Load()
}

// Close shuts the server down. Safe to call once per server.
func (c *configURLServer) Close() {
	c.server.Close()
}

var _ = Describe("kairos install test", Label("install"), func() {

	var vm VM
	BeforeEach(func() {
		_, vm = startVM()
		vm.EventuallyConnects(1200)
	})

	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			serial, _ := os.ReadFile(filepath.Join(vm.StateDir, "serial.log"))
			_ = os.MkdirAll("logs", os.ModePerm|os.ModeDir)
			_ = os.WriteFile(filepath.Join("logs", "serial.log"), serial, os.ModePerm)
			fmt.Println(string(serial))
		}

		if CurrentSpecReport().Failed() {
			gatherLogs(vm)
		}
		Expect(vm.Destroy(nil)).ToNot(HaveOccurred())
	})

	Context("install", func() {
		It("cloud-config syntax mixed with extended syntax", func() {

			expectSecureBootEnabled(vm)

			_ = testInstall(`#cloud-config
install:
  bind_mounts:
  - /var/bind1
  - /var/bind2
  ephemeral_mounts:
  - /var/ephemeral
  - /var/ephemeral2
users:
- name: "kairos"
  passwd: "kairos"
stages:
  initramfs:
  - name: "Set user and password"
    users:
      kairos:
         passwd: "kairos"
         groups:
           - "admin"
    commands:
    - echo "bar" > /etc/foo
bundles:
- rootfs_path: "/usr/local/bin"
  targets:
  - container://quay.io/mocaccino/extra:edgevpn-utils-0.15.0
`, vm)

			expectSecureBootEnabled(vm)

			Eventually(func() string {
				out, _ := vm.Sudo("cat /etc/foo")
				return out
			}, 5*time.Minute, 10*time.Second).Should(ContainSubstring("bar"))

			Eventually(func() string {
				out, _ := vm.Sudo("cat /run/cos/cos-layout.env")
				return out
			}, 5*time.Minute, 10*time.Second).Should(ContainSubstring("/var/bind1 /var/bind2"))
			Eventually(func() string {
				out, _ := vm.Sudo("cat /run/cos/cos-layout.env")
				return out
			}, 5*time.Minute, 10*time.Second).Should(ContainSubstring("/var/ephemeral /var/ephemeral2"))

			Eventually(func() string {
				out, _ := vm.Sudo("/usr/local/bin/usr/bin/edgevpn --help | grep peer")
				return out
			}, 5*time.Minute, 10*time.Second).Should(ContainSubstring("peerguard"))

			stateAssertVM(vm, "persistent.found", "true")

			By("Checking the multi-call binary layout", func() {
				out, err := vm.Sudo("test -f /usr/bin/kairos && ! test -L /usr/bin/kairos && echo ok")
				Expect(err).ToNot(HaveOccurred(), out)
				Expect(out).To(ContainSubstring("ok"), "/usr/bin/kairos should be a real file, not a symlink")

				for _, link := range []string{
					"/usr/bin/kairos-agent",
					"/usr/bin/immucore",
					"/system/discovery/kcrypt-discovery-challenger",
				} {
					// peg's Sudo merges the session's stdout and stderr into
					// a single buffer (stdout copied first, then stderr), and
					// the stderr side picks up unrelated diagnostics from
					// `sudo /bin/sh` itself -- notably Ubuntu's default
					// "unable to resolve host <hostname>" warning when the
					// installed hostname isn't in /etc/hosts. That warning
					// is emitted by sudo before /bin/sh ever runs, so
					// redirecting readlink's own stderr does not silence it.
					// Match against the first line of the buffer instead of
					// the whole thing: readlink prints exactly one line
					// (the resolved path) to stdout, and stdout is copied
					// before stderr, so line 1 is always the answer.
					out, err := vm.Sudo(fmt.Sprintf("readlink -f %s", link))
					Expect(err).ToNot(HaveOccurred(), out)
					first := strings.SplitN(strings.TrimSpace(out), "\n", 2)[0]
					Expect(strings.TrimSpace(first)).To(Equal("/usr/bin/kairos"),
						"expected %s to resolve to /usr/bin/kairos, got %q", link, out)
					// And confirm it is a symlink, not a real duplicate.
					out, err = vm.Sudo(fmt.Sprintf("test -L %s && echo symlink", link))
					Expect(err).ToNot(HaveOccurred(), out)
					Expect(out).To(ContainSubstring("symlink"),
						"%s should be a symlink to /usr/bin/kairos", link)
				}
			})

			By("Checking every component reports one version", func() {
				// Same stdout/stderr merging as the readlink note above.
				firstLine := func(s string) string {
					return strings.TrimSpace(strings.SplitN(strings.TrimSpace(s), "\n", 2)[0])
				}

				out, err := vm.Sudo("/usr/bin/kairos --version")
				Expect(err).ToNot(HaveOccurred(), out)
				reported := firstLine(out)
				Expect(reported).To(HavePrefix("kairos "), out)

				version := strings.TrimSpace(strings.TrimPrefix(reported, "kairos"))
				Expect(version).ToNot(BeEmpty())
				Expect(version).ToNot(Equal("dev"), "the build did not stamp a version")

				for _, link := range []string{
					"/usr/bin/kairos-agent",
					"/usr/bin/immucore",
					"/system/discovery/kcrypt-discovery-challenger",
				} {
					out, err := vm.Sudo(fmt.Sprintf("%s --version", link))
					Expect(err).ToNot(HaveOccurred(), out)
					Expect(firstLine(out)).To(Equal(reported), link)
				}

				// kairos-init is not installed here; the version it reported at
				// build time survives only in what it wrote to kairos-release.
				out, err = vm.Sudo(". /etc/kairos-release; echo $KAIROS_INIT_VERSION")
				Expect(err).ToNot(HaveOccurred(), out)
				Expect(firstLine(out)).To(Equal(version),
					"KAIROS_INIT_VERSION disagrees with the kairos binary")
			})

			By("Checking install/recovery services are disabled", func() {
				if !isFlavor(vm, "alpine") {
					for _, service := range []string{"kairos-interactive", "kairos-recovery"} {
						By(fmt.Sprintf("Checking that service %s does not exist", service), func() {})
						Eventually(func() string {
							out, _ := vm.Sudo(fmt.Sprintf("systemctl status %s", service))
							return out
						}, 3*time.Minute, 2*time.Second).Should(
							And(
								ContainSubstring(fmt.Sprintf("Unit %s.service could not be found", service)),
							),
						)
					}
				}
			})
		})

		Context("with config_url", func() {
			var payloadServer *configURLServer

			AfterEach(func() {
				if payloadServer != nil {
					payloadServer.Close()
					payloadServer = nil
				}
			})

			It("succeeds when config_url is accessible", func() {
				payloadServer = startConfigURLServer()

				testInstall(fmt.Sprintf(`#cloud-config
config_url: "%s"
users:
- name: "kairos"
  passwd: "kairos"
  groups:
    - "admin"
`, payloadServer.URL()), vm)

				Eventually(func() string {
					out, err := vm.Sudo("kairos-agent state")
					Expect(err).ToNot(HaveOccurred())
					return out
				}, 5*time.Minute, 10*time.Second).Should(ContainSubstring("boot: active_boot"))

				// The state check above says the machine booted, not that the
				// remote config was ever merged. The payload writes a marker
				// from a stage nothing else in this cell declares, so the
				// marker existing is only explainable by the fetch and the
				// merge both having happened.
				By("Checking the remote payload was applied", func() {
					Eventually(func() string {
						out, _ := vm.Sudo("cat " + configURLMarkerPath)
						return out
					}, 5*time.Minute, 10*time.Second).Should(ContainSubstring(configURLMarkerContent),
						"the config_url payload was not applied: %s is missing", configURLMarkerPath)
				})

				// And the server really was asked for it, which separates "the
				// guest applied our payload" from "the guest already had a
				// marker lying around".
				Expect(payloadServer.Hits()).To(BeNumerically(">", 0),
					"the guest never fetched the config_url payload")
			})

			It("succeeds when config_url is not accessible (and prints a warning)", func() {
				out := testInstall(`#cloud-config
config_url: "https://thisurldoesntexist.org"
users:
- name: "kairos"
  passwd: "kairos"
  groups:
    - "admin"
`, vm)
				Expect(out).ToNot(ContainSubstring("kairos-agent.service: Failed with result"))

				Eventually(func() string {
					out, err := vm.Sudo("kairos-agent state")
					Expect(err).ToNot(HaveOccurred())
					return out
				}, 5*time.Minute, 10*time.Second).Should(ContainSubstring("boot: active_boot"))

				// The counterpart of the assertion above. Without this the two
				// cells pass on identical evidence and cannot tell a payload
				// that was applied from one that was silently dropped.
				By("Checking no payload was applied", func() {
					out, _ := vm.Sudo("test -e " + configURLMarkerPath + " && echo present || echo absent")
					Expect(out).To(ContainSubstring("absent"),
						"%s exists although no config_url was reachable", configURLMarkerPath)
				})
			})
		})
	})
})
