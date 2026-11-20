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

var _ = Describe("Import Flow E2E", Label("e2e", "import"), func() {
	var (
		vm        VM
		vmStarted bool
		stateDir  string
	)

	BeforeEach(func() {
		if vanillaISO == "" {
			Skip("No vanilla ISO available")
		}
		vmStarted = false
		stateDir = filepath.Join(tmpDir, "vm-import")
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

	It("should boot a vanilla VM and register via import script", func() {
		By("Creating datasource with auto-install only (no auroraboot config)")
		cloudConfig := `#cloud-config
install:
  auto: true
  device: /dev/vda
  reboot: true
stages:
  initramfs:
    - users:
        kairos:
          passwd: kairos
          groups:
            - admin
`
		datasource := createDatasource(cloudConfig)
		defer os.RemoveAll(filepath.Dir(datasource))

		By("Booting vanilla VM (no auroraboot config)")
		vm = startVM(vanillaISO, stateDir, datasource)
		vmStarted = true

		By("Waiting for VM to become accessible via SSH (install + reboot)")
		vm.EventuallyConnects(600)

		By("Verifying Kairos installed but node is NOT registered")
		out, err := vm.Sudo("cat /etc/kairos-release")
		Expect(err).ToNot(HaveOccurred(), out)
		Expect(out).To(ContainSubstring("KAIROS"))

		// Count nodes before import
		resp := adminGet("/api/v1/nodes")
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		nodesBefore := decodeJSONArray(resp)
		countBefore := len(nodesBefore)

		By("Getting the install-agent script from auroraboot")
		resp = adminGet("/api/v1/install-agent")
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		script := readBody(resp)
		Expect(script).To(ContainSubstring("AURORABOOT_URL"))

		// Replace the host URL with VM-accessible URL in the script
		script = strings.Replace(script, aurorabootURL, vmAuroraBootURL, -1)

		By("Writing the install script to the VM")
		scriptFile := filepath.Join(tmpDir, "install-auroraboot.sh")
		Expect(os.WriteFile(scriptFile, []byte(script), 0755)).To(Succeed())
		err = vm.Scp(scriptFile, "/tmp/install-auroraboot.sh", "0755")
		Expect(err).ToNot(HaveOccurred())

		By("Running the import script on the VM")
		out, err = vm.Sudo(fmt.Sprintf(
			"AURORABOOT_URL=%s REGISTRATION_TOKEN=%s sh /tmp/install-auroraboot.sh",
			vmAuroraBootURL, regToken,
		))
		Expect(err).ToNot(HaveOccurred(), out)
		GinkgoWriter.Printf("Import script output: %s\n", out)

		// The install script writes /oem/auroraboot.yaml and runs kairos-agent start
		// which auto-installs and starts the phone-home service. No manual intervention needed.
		time.Sleep(10 * time.Second)

		By("Waiting for the imported node to appear in auroraboot")
		Eventually(func() int {
			resp := adminGet("/api/v1/nodes")
			if resp.StatusCode != http.StatusOK {
				resp.Body.Close()
				return countBefore
			}
			nodes := decodeJSONArray(resp)
			return len(nodes)
		}, 120*time.Second, 5*time.Second).Should(BeNumerically(">", countBefore))

		By("Verifying the imported node is Online")
		resp = adminGet("/api/v1/nodes")
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		nodes := decodeJSONArray(resp)

		var importedNode map[string]interface{}
		for _, n := range nodes {
			// Find the newly imported node (not the one from call-home test)
			found := false
			for _, before := range nodesBefore {
				if before["id"] == n["id"] {
					found = true
					break
				}
			}
			if !found {
				importedNode = n
				break
			}
		}
		Expect(importedNode).ToNot(BeNil(), "Imported node not found")

		nodeID := importedNode["id"].(string)
		GinkgoWriter.Printf("Imported node ID: %s\n", nodeID)

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
		}, 60*time.Second, 5*time.Second).Should(Equal("Online"))

		By("Running a command on the imported node")
		resp = adminPost(fmt.Sprintf("/api/v1/nodes/%s/commands", nodeID), map[string]interface{}{
			"command": "exec",
			"args":    map[string]string{"command": "hostname"},
		})
		Expect(resp.StatusCode).To(Equal(http.StatusCreated))
		cmd := decodeJSON(resp)
		cmdID := cmd["id"].(string)

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
					if p, ok := c["phase"].(string); ok {
						return p
					}
				}
			}
			return ""
		}, 60*time.Second, 2*time.Second).Should(Equal("Completed"))
	})
})
