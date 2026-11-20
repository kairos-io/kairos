package e2e_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/spectrocloud/peg/matcher"
)

// Decommission E2E exercises the remote teardown end-to-end against a real
// Kairos VM: boot → register → POST /decommission → agent runs the
// `unregister` command → SSH in and verify every phonehome artifact is gone
// → DELETE → verify the record is gone on the server too. Also covers the
// offline path (force-delete + local CLI uninstall) on the same VM.
var _ = Describe("Decommission E2E Flow", Label("e2e", "decommission"), func() {
	var (
		vm        VM
		vmStarted bool
		stateDir  string
	)

	BeforeEach(func() {
		if callHomeISO == "" {
			Skip("No call-home ISO available")
		}
		vmStarted = false
		stateDir = filepath.Join(tmpDir, "vm-decommission")
	})

	AfterEach(func() {
		if vmStarted {
			vm.GatherAllLogs(
				[]string{"kairos-agent", "kairos-agent-phonehome"},
				[]string{"/var/log/kairos/*.log"},
			)
			vm.Destroy(nil)
		}
	})

	It("tears down the phone-home install end-to-end (online + offline paths)", func() {
		By("Creating datasource with auto-install + phonehome config")
		cloudConfig := fmt.Sprintf(`#cloud-config
install:
  auto: true
  device: /dev/vda
  reboot: true
phonehome:
  url: "%s"
  registration_token: "%s"
  group: "e2e-decommission"
stages:
  initramfs:
    - users:
        kairos:
          passwd: kairos
          groups:
            - admin
`, vmAuroraBootURL, regToken)
		datasource := createDatasource(cloudConfig)
		defer os.RemoveAll(filepath.Dir(datasource))

		By("Booting VM from call-home ISO")
		vm = startVM(callHomeISO, stateDir, datasource)
		vmStarted = true
		vm.EventuallyConnects(600)

		By("Waiting for kairos-agent-phonehome to come up and register")
		Eventually(func() string {
			out, _ := vm.Sudo("systemctl is-active kairos-agent-phonehome 2>&1")
			return strings.TrimSpace(out)
		}, 120*time.Second, 5*time.Second).Should(Equal("active"))

		var nodeID string
		Eventually(func() bool {
			resp := adminGet("/api/v1/nodes")
			if resp.StatusCode != http.StatusOK {
				resp.Body.Close()
				return false
			}
			for _, n := range decodeJSONArray(resp) {
				id, _ := n["id"].(string)
				if id != "" {
					nodeID = id
					return true
				}
			}
			return false
		}, 120*time.Second, 5*time.Second).Should(BeTrue())
		GinkgoWriter.Printf("Registered node ID: %s\n", nodeID)

		By("Waiting for node phase Online")
		Eventually(func() string {
			resp := adminGet("/api/v1/nodes/" + nodeID)
			if resp.StatusCode != http.StatusOK {
				resp.Body.Close()
				return ""
			}
			return decodeJSON(resp)["phase"].(string)
		}, 120*time.Second, 5*time.Second).Should(Equal("Online"))

		// ---------- Online decommission path ----------

		By("POST /decommission against the online node")
		resp := adminPost("/api/v1/nodes/"+nodeID+"/decommission", nil)
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		dec := decodeJSON(resp)
		Expect(dec["nodeOnline"]).To(Equal(true))
		cmdID, _ := dec["commandID"].(string)
		Expect(cmdID).ToNot(BeEmpty())
		GinkgoWriter.Printf("Dispatched unregister commandID=%s\n", cmdID)

		By("Waiting for the unregister command to report Completed")
		Eventually(func() string {
			resp := adminGet("/api/v1/nodes/" + nodeID + "/commands")
			if resp.StatusCode != http.StatusOK {
				resp.Body.Close()
				return ""
			}
			var cmds []map[string]interface{}
			if err := json.Unmarshal([]byte(readBody(resp)), &cmds); err != nil {
				return ""
			}
			for _, c := range cmds {
				if c["id"] == cmdID {
					if r, ok := c["result"].(string); ok && r != "" {
						GinkgoWriter.Printf("Agent teardown summary:\n%s\n", r)
					}
					if p, ok := c["phase"].(string); ok {
						return p
					}
				}
			}
			return ""
		}, 60*time.Second, 2*time.Second).Should(Equal("Completed"))

		By("SSH into the VM and verify every phonehome artifact is gone")
		// Service should no longer be active. systemctl returns non-zero for
		// both "inactive" and "unknown" — we only care that it is NOT active.
		activeOut, _ := vm.Sudo("systemctl is-active kairos-agent-phonehome 2>&1 || true")
		Expect(strings.TrimSpace(activeOut)).ToNot(Equal("active"),
			"phonehome service should be stopped after unregister; got: %s", activeOut)

		// Unit file gone.
		_, err := vm.Sudo("test -f /etc/systemd/system/kairos-agent-phonehome.service")
		Expect(err).To(HaveOccurred(), "unit file must be removed")

		// Credentials file gone.
		_, err = vm.Sudo("test -f /usr/local/.kairos/phonehome-credentials.yaml")
		Expect(err).To(HaveOccurred(), "credentials file must be removed")

		// Primary cloud-config gone (the one baked into /oem by the datasource).
		// Note: if the file lived elsewhere because of a bespoke datasource
		// layout, the agent's Uninstall only knows about /oem/phonehome.yaml —
		// the absence of that exact path is what we assert.
		_, err = vm.Sudo("test -f /oem/phonehome.yaml")
		Expect(err).To(HaveOccurred(), "/oem/phonehome.yaml must be removed")

		// No phone-home process. `pgrep` exits non-zero when nothing matches.
		_, err = vm.Sudo("pgrep -f 'kairos-agent phone-home'")
		Expect(err).To(HaveOccurred(), "no kairos-agent phone-home process should remain")

		By("DELETE /api/v1/nodes/:id finalizes server-side cleanup")
		delResp := adminDelete("/api/v1/nodes/" + nodeID)
		Expect(delResp.StatusCode).To(Equal(http.StatusNoContent))
		delResp.Body.Close()
		getResp := adminGet("/api/v1/nodes/" + nodeID)
		Expect(getResp.StatusCode).To(Equal(http.StatusNotFound))
		getResp.Body.Close()

		// ---------- Offline + local CLI fallback path ----------
		//
		// The phonehome install is already gone after the online path, so
		// re-enable just enough to confirm the local CLI (`kairos-agent
		// phone-home uninstall`) would do the same teardown on a box that
		// never got a remote unregister. We simulate the situation by
		// writing the credentials file and a service unit back by hand,
		// then running the CLI and re-asserting the checklist.

		By("Simulating a leftover phonehome install on the VM")
		_, err = vm.Sudo("mkdir -p /usr/local/.kairos && echo 'node_id: synthetic' | tee /usr/local/.kairos/phonehome-credentials.yaml")
		Expect(err).ToNot(HaveOccurred())
		_, err = vm.Sudo("mkdir -p /oem && echo '#cloud-config' | tee /oem/phonehome.yaml")
		Expect(err).ToNot(HaveOccurred())
		_, err = vm.Sudo("printf '[Unit]\\nDescription=placeholder\\n[Service]\\nExecStart=/bin/true\\n' | tee /etc/systemd/system/kairos-agent-phonehome.service && systemctl daemon-reload")
		Expect(err).ToNot(HaveOccurred())

		By("Running `kairos-agent phone-home uninstall` locally")
		out, err := vm.Sudo("kairos-agent phone-home uninstall")
		Expect(err).ToNot(HaveOccurred(), out)
		GinkgoWriter.Printf("Local uninstall output:\n%s\n", out)

		_, err = vm.Sudo("test -f /etc/systemd/system/kairos-agent-phonehome.service")
		Expect(err).To(HaveOccurred(), "unit file must be removed by CLI too")
		_, err = vm.Sudo("test -f /usr/local/.kairos/phonehome-credentials.yaml")
		Expect(err).To(HaveOccurred(), "credentials file must be removed by CLI too")
		_, err = vm.Sudo("test -f /oem/phonehome.yaml")
		Expect(err).To(HaveOccurred(), "/oem/phonehome.yaml must be removed by CLI too")

		By("Running the CLI a second time is a no-op (idempotent)")
		out, err = vm.Sudo("kairos-agent phone-home uninstall")
		Expect(err).ToNot(HaveOccurred(), out)
		Expect(out).To(ContainSubstring("already absent"))
	})
})
