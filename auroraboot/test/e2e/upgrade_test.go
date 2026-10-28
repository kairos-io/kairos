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

var _ = Describe("Upgrade E2E Flow", Label("e2e", "upgrade"), func() {
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
		stateDir = filepath.Join(tmpDir, "vm-upgrade")
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

	It("should build an artifact, register a node, and upgrade it from the artifact", func() {
		// --- Phase 1: Build artifact via API ---
		By("Creating an artifact build via the API")

		// Use the same base image as the test ISO.
		// If we have kairosBaseImage, use that; otherwise derive from the ISO's os-release.
		baseImage := kairosBaseImage
		if baseImage == "" || strings.HasPrefix(baseImage, "docker:") {
			// Strip docker: prefix for the API
			baseImage = strings.TrimPrefix(baseImage, "docker:")
		}

		resp := adminPost("/api/v1/artifacts", map[string]interface{}{
			"name":          "e2e-upgrade-test",
			"baseImage":     baseImage,
			"kairosVersion": "e2e-test",
			"model":         "generic",
			"iso":           false,
		})
		Expect(resp.StatusCode).To(Equal(http.StatusCreated))
		artifact := decodeJSON(resp)
		artifactID := artifact["id"].(string)
		GinkgoWriter.Printf("Created artifact: %s\n", artifactID)

		By("Waiting for artifact build to complete")
		Eventually(func() string {
			resp := adminGet(fmt.Sprintf("/api/v1/artifacts/%s", artifactID))
			if resp.StatusCode != http.StatusOK {
				resp.Body.Close()
				return ""
			}
			a := decodeJSON(resp)
			phase := a["phase"].(string)
			if phase == "Error" {
				msg, _ := a["message"].(string)
				GinkgoWriter.Printf("Artifact build error: %s\n", msg)
			}
			return phase
		}, 10*time.Minute, 10*time.Second).Should(Equal("Ready"))

		By("Verifying artifact has a container image")
		resp = adminGet(fmt.Sprintf("/api/v1/artifacts/%s", artifactID))
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		artifact = decodeJSON(resp)
		containerImage, _ := artifact["containerImage"].(string)
		Expect(containerImage).ToNot(BeEmpty(), "Artifact should have a containerImage after build")
		GinkgoWriter.Printf("Artifact container image: %s\n", containerImage)

		// --- Phase 2: Boot VM and register node ---
		By("Creating datasource with auroraboot config")
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

		By("Booting VM")
		vm = startVM(callHomeISO, stateDir, datasource)
		vmStarted = true

		By("Waiting for VM to become accessible")
		vm.EventuallyConnects(600)

		By("Waiting for node to register and come Online")
		var nodeID string
		Eventually(func() bool {
			resp := adminGet("/api/v1/nodes")
			if resp.StatusCode != http.StatusOK {
				resp.Body.Close()
				return false
			}
			nodes := decodeJSONArray(resp)
			for _, n := range nodes {
				nodeID = n["id"].(string)
			}
			return nodeID != ""
		}, 120*time.Second, 5*time.Second).Should(BeTrue())
		GinkgoWriter.Printf("Registered node: %s\n", nodeID)

		Eventually(func() string {
			resp := adminGet(fmt.Sprintf("/api/v1/nodes/%s", nodeID))
			if resp.StatusCode != http.StatusOK {
				resp.Body.Close()
				return ""
			}
			n := decodeJSON(resp)
			p, _ := n["phase"].(string)
			return p
		}, 120*time.Second, 5*time.Second).Should(Equal("Online"))

		// --- Phase 3: Trigger upgrade from artifact ---
		By("Sending upgrade command using the built artifact")
		resp = adminPost(fmt.Sprintf("/api/v1/nodes/%s/commands", nodeID), map[string]interface{}{
			"command": "upgrade",
			"args":    map[string]string{"source": "artifact:" + artifactID},
		})
		Expect(resp.StatusCode).To(Equal(http.StatusCreated))
		cmd := decodeJSON(resp)
		cmdID := cmd["id"].(string)
		GinkgoWriter.Printf("Upgrade command ID: %s\n", cmdID)

		By("Waiting for upgrade command to be picked up (Running or Completed)")
		Eventually(func() string {
			resp := adminGet(fmt.Sprintf("/api/v1/nodes/%s/commands", nodeID))
			if resp.StatusCode != http.StatusOK {
				resp.Body.Close()
				return ""
			}
			body := readBody(resp)
			var cmds []map[string]interface{}
			if err := json.Unmarshal([]byte(body), &cmds); err != nil {
				return ""
			}
			for _, c := range cmds {
				if c["id"] == cmdID {
					p, _ := c["phase"].(string)
					GinkgoWriter.Printf("Upgrade command phase: %s\n", p)
					return p
				}
			}
			return ""
		}, 5*time.Minute, 10*time.Second).Should(SatisfyAny(
			Equal("Completed"),
			Equal("Failed"),
		))

		By("Checking upgrade command result")
		resp = adminGet(fmt.Sprintf("/api/v1/nodes/%s/commands", nodeID))
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		body := readBody(resp)
		var allCmds []map[string]interface{}
		Expect(json.Unmarshal([]byte(body), &allCmds)).To(Succeed())
		for _, c := range allCmds {
			if c["id"] == cmdID {
				phase := c["phase"].(string)
				result, _ := c["result"].(string)
				GinkgoWriter.Printf("Upgrade result (phase=%s):\n%s\n", phase, result)
				Expect(phase).To(Equal("Completed"), "Upgrade command should complete successfully")
			}
		}

		// --- Phase 4: Wait for reboot after upgrade ---
		By("Waiting for node to go Offline (reboot after upgrade)")
		Eventually(func() string {
			resp := adminGet(fmt.Sprintf("/api/v1/nodes/%s", nodeID))
			if resp.StatusCode != http.StatusOK {
				resp.Body.Close()
				return ""
			}
			n := decodeJSON(resp)
			p, _ := n["phase"].(string)
			return p
		}, 60*time.Second, 3*time.Second).Should(Equal("Offline"))

		By("Waiting for VM to come back via SSH after upgrade reboot")
		vm.EventuallyConnects(300)

		By("Waiting for node to come back Online after upgrade")
		Eventually(func() string {
			resp := adminGet(fmt.Sprintf("/api/v1/nodes/%s", nodeID))
			if resp.StatusCode != http.StatusOK {
				resp.Body.Close()
				return ""
			}
			n := decodeJSON(resp)
			p, _ := n["phase"].(string)
			return p
		}, 120*time.Second, 5*time.Second).Should(Equal("Online"))

		By("Verifying the node is functional after upgrade")
		resp = adminPost(fmt.Sprintf("/api/v1/nodes/%s/commands", nodeID), map[string]interface{}{
			"command": "exec",
			"args":    map[string]string{"command": "echo upgrade-success > /tmp/upgrade-marker"},
		})
		Expect(resp.StatusCode).To(Equal(http.StatusCreated))
		postCmd := decodeJSON(resp)
		postCmdID := postCmd["id"].(string)

		Eventually(func() string {
			resp := adminGet(fmt.Sprintf("/api/v1/nodes/%s/commands", nodeID))
			if resp.StatusCode != http.StatusOK {
				resp.Body.Close()
				return ""
			}
			body := readBody(resp)
			var cmds []map[string]interface{}
			if err := json.Unmarshal([]byte(body), &cmds); err != nil {
				return ""
			}
			for _, c := range cmds {
				if c["id"] == postCmdID {
					p, _ := c["phase"].(string)
					return p
				}
			}
			return ""
		}, 60*time.Second, 2*time.Second).Should(Equal("Completed"))

		out, err := vm.Sudo("cat /tmp/upgrade-marker")
		Expect(err).ToNot(HaveOccurred(), out)
		Expect(out).To(ContainSubstring("upgrade-success"))
		GinkgoWriter.Println("Node functional after upgrade!")
	})
})
