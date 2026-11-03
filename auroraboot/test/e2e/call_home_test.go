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

var _ = Describe("Call-Home E2E Flow", Label("e2e", "call-home"), func() {
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
		stateDir = filepath.Join(tmpDir, "vm-callhome")
	})

	AfterEach(func() {
		if vmStarted {
			vm.GatherAllLogs(
				[]string{"kairos-agent"},
				[]string{"/var/log/kairos/*.log"},
			)
			vm.Destroy(nil)
		}
	})

	It("should boot, auto-install, register with auroraboot, and execute commands", func() {
		By("Creating datasource with auto-install + auroraboot config")
		cloudConfig := fmt.Sprintf(`#cloud-config
install:
  auto: true
  device: /dev/vda
  reboot: true
phonehome:
  url: "%s"
  registration_token: "%s"
  group: "e2e-test"
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

		By("Booting VM from call-home ISO with datasource")
		vm = startVM(callHomeISO, stateDir, datasource)
		vmStarted = true

		By("Waiting for VM to become accessible via SSH (install + reboot)")
		vm.EventuallyConnects(600) // 10 min for install + reboot

		By("Verifying Kairos installed successfully")
		out, err := vm.Sudo("cat /etc/kairos-release")
		Expect(err).ToNot(HaveOccurred(), out)
		Expect(out).To(ContainSubstring("KAIROS"))

		By("Waiting for phone-home service to be auto-started by kairos-agent start")
		// The auroraboot config is in /oem/95_userdata/userdata.yaml (from datasource).
		// Kairos init runs kairos-agent start on boot, which calls enablePhoneHomeIfConfigured()
		// to auto-install and start the phone-home systemd service.
		// This may take a moment after SSH becomes available.
		Eventually(func() string {
			out, _ := vm.Sudo("systemctl is-active kairos-agent-phonehome 2>&1")
			return strings.TrimSpace(out)
		}, 120*time.Second, 5*time.Second).Should(Equal("active"))

		out, _ = vm.Sudo("systemctl status kairos-agent-phonehome 2>&1")
		GinkgoWriter.Printf("Phone-home service:\n%s\n", out)

		By("Waiting for node to register with auroraboot")
		var nodeID string
		Eventually(func() bool {
			resp := adminGet("/api/v1/nodes")
			if resp.StatusCode != http.StatusOK {
				resp.Body.Close()
				return false
			}
			nodes := decodeJSONArray(resp)
			for _, n := range nodes {
				if g, _ := n["groupID"].(string); g != "" {
					// Find our node by checking it was recently created
					nodeID = n["id"].(string)
					return true
				}
				// Or just take any node that appeared during this test
				nodeID = n["id"].(string)
			}
			return nodeID != ""
		}, 120*time.Second, 5*time.Second).Should(BeTrue())
		GinkgoWriter.Printf("Registered node ID: %s\n", nodeID)

		By("Waiting for node to come Online (WebSocket connected)")
		Eventually(func() string {
			resp := adminGet(fmt.Sprintf("/api/v1/nodes/%s", nodeID))
			if resp.StatusCode != http.StatusOK {
				resp.Body.Close()
				return ""
			}
			n := decodeJSON(resp)
			if p, ok := n["phase"].(string); ok {
				return p
			}
			return ""
		}, 120*time.Second, 5*time.Second).Should(Equal("Online"))

		By("Verifying node reports agent version")
		resp := adminGet(fmt.Sprintf("/api/v1/nodes/%s", nodeID))
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		node := decodeJSON(resp)
		GinkgoWriter.Printf("Node details: %v\n", node)

		By("Sending exec command to the node via auroraboot API")
		resp = adminPost(fmt.Sprintf("/api/v1/nodes/%s/commands", nodeID), map[string]interface{}{
			"command": "exec",
			"args":    map[string]string{"command": "echo auroraboot-e2e-test > /tmp/auroraboot-marker"},
		})
		Expect(resp.StatusCode).To(Equal(http.StatusCreated))
		cmd := decodeJSON(resp)
		cmdID := cmd["id"].(string)

		By("Waiting for command to complete")
		// Debug: check command phase immediately
		time.Sleep(2 * time.Second)
		debugResp := adminGet(fmt.Sprintf("/api/v1/nodes/%s/commands", nodeID))
		GinkgoWriter.Printf("Commands after 2s: %s\n", readBody(debugResp))

		// Check service journal for command execution
		out, _ = vm.Sudo("journalctl -u kairos-phonehome --no-pager -n 30 2>&1")
		GinkgoWriter.Printf("Agent journal after command:\n%s\n", out)

		Eventually(func() string {
			resp := adminGet(fmt.Sprintf("/api/v1/nodes/%s/commands", nodeID))
			if resp.StatusCode != http.StatusOK {
				resp.Body.Close()
				return ""
			}
			// Parse as array, find our command
			var cmds []map[string]interface{}
			body := readBody(resp)
			if err := json.Unmarshal([]byte(body), &cmds); err != nil {
				return ""
			}
			for _, c := range cmds {
				if c["id"] == cmdID {
					if p, ok := c["phase"].(string); ok {
						return p
					}
				}
			}
			return ""
		}, 60*time.Second, 2*time.Second).Should(Equal("Completed"))

		By("Verifying the command actually executed on the VM via SSH")
		out, err = vm.Sudo("cat /tmp/auroraboot-marker")
		Expect(err).ToNot(HaveOccurred(), out)
		Expect(out).To(ContainSubstring("auroraboot-e2e-test"))

		By("Rebooting VM to test reconnection")
		vm.Sudo("reboot")
		// reboot will disconnect SSH, that's expected

		By("Waiting for node to go Offline after reboot")
		Eventually(func() string {
			resp := adminGet(fmt.Sprintf("/api/v1/nodes/%s", nodeID))
			if resp.StatusCode != http.StatusOK {
				resp.Body.Close()
				return ""
			}
			n := decodeJSON(resp)
			if p, ok := n["phase"].(string); ok {
				return p
			}
			return ""
		}, 60*time.Second, 3*time.Second).Should(Equal("Offline"))
		GinkgoWriter.Println("Node went Offline after reboot")

		By("Waiting for VM to come back up via SSH")
		vm.EventuallyConnects(300) // 5 min for reboot

		By("Checking that kairos-agent start auto-installs phone-home service after reboot")
		// After reboot, Kairos init runs kairos-agent start, which should:
		// 1. Find auroraboot config in /oem/95_userdata/userdata.yaml
		// 2. Auto-install and start the kairos-agent-phonehome systemd service
		// Give it time to boot and run the agent
		time.Sleep(10 * time.Second)
		out, _ = vm.Sudo("systemctl status kairos-agent-phonehome 2>&1 || true")
		GinkgoWriter.Printf("Auto-started service after reboot:\n%s\n", out)

		By("Waiting for node to come back Online")
		Eventually(func() string {
			resp := adminGet(fmt.Sprintf("/api/v1/nodes/%s", nodeID))
			if resp.StatusCode != http.StatusOK {
				resp.Body.Close()
				return ""
			}
			n := decodeJSON(resp)
			if p, ok := n["phase"].(string); ok {
				return p
			}
			return ""
		}, 120*time.Second, 5*time.Second).Should(Equal("Online"))
		GinkgoWriter.Println("Node is back Online after reboot")

		By("Sending a command after reboot to verify agent works")
		resp = adminPost(fmt.Sprintf("/api/v1/nodes/%s/commands", nodeID), map[string]interface{}{
			"command": "exec",
			"args":    map[string]string{"command": "echo reboot-test > /tmp/reboot-marker"},
		})
		Expect(resp.StatusCode).To(Equal(http.StatusCreated))
		cmd2 := decodeJSON(resp)
		cmd2ID := cmd2["id"].(string)

		By("Waiting for post-reboot command to complete")
		Eventually(func() string {
			resp := adminGet(fmt.Sprintf("/api/v1/nodes/%s/commands", nodeID))
			if resp.StatusCode != http.StatusOK {
				resp.Body.Close()
				return ""
			}
			var cmds []map[string]interface{}
			body := readBody(resp)
			if err := json.Unmarshal([]byte(body), &cmds); err != nil {
				return ""
			}
			for _, c := range cmds {
				if c["id"] == cmd2ID {
					if p, ok := c["phase"].(string); ok {
						return p
					}
				}
			}
			return ""
		}, 60*time.Second, 2*time.Second).Should(Equal("Completed"))

		By("Verifying post-reboot command executed on VM")
		out, err = vm.Sudo("cat /tmp/reboot-marker")
		Expect(err).ToNot(HaveOccurred(), out)
		Expect(out).To(ContainSubstring("reboot-test"))
		GinkgoWriter.Println("Post-reboot command executed successfully!")
	})
})
