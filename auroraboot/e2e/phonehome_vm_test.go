package e2e_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gofrs/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/format"
)

// This tier runs AuroraBoot in a container and two QEMU VMs installed from an
// ISO AuroraBoot built, and drives batch upgrades of the VMs through the admin
// API. Containers cannot reboot and do not run an agent built by AuroraBoot's
// kairos-init step, so this is the only tier that sees the agent download an
// artifact image, upgrade, reboot and report back.
//
// Inputs, all optional:
//   - PHONEHOME_BASE_IMAGE: the Kairos image both artifacts are built from. Its
//     agent must find the persistent partition on a running system, because
//     the upgrade target is built from it without kairos-init and the nodes
//     run its agent after the upgrade.
//   - PHONEHOME_KAIROS_INIT_IMAGE: the kairos-init image of the installed
//     artifact. AuroraBoot's default kairos-init ships an agent that cannot
//     download artifact images.
//   - PHONEHOME_LOG_DIR: where a failed run leaves AuroraBoot's log and each
//     VM's logs.
//   - KVM, FIRMWARE, MEMORY and CPUS, as for the other VM tiers. MEMORY and
//     CPUS default lower here because two VMs run at once.

const (
	// phonehomeKairosVersion is the Kairos release the tier runs. The base
	// image, the kairos-init image and the version kairos-init stamps must all
	// agree: a kairos-init from another release replaces the base image's agent
	// with its own. It is also sent as the artifact's kairosVersion because the
	// API falls back to "latest" without one, which kairos-init v4.4.0 refuses
	// as not a semantic version; the UI always sends a version.
	phonehomeKairosVersion          = "v4.4.0-rc2"
	phonehomeDefaultBaseImage       = "quay.io/kairos/hadron:v0.5.3-core-amd64-generic-" + phonehomeKairosVersion
	phonehomeDefaultKairosInitImage = "quay.io/kairos/kairos-init:" + phonehomeKairosVersion
	phonehomeMarkerPath             = "/etc/auroraboot-e2e-marker"
	// phonehomeUnknownArtifact is an artifact ID AuroraBoot does not know, so
	// an upgrade from it fails on AuroraBoot's own image download, before the
	// node reboots.
	phonehomeUnknownArtifact = "00000000-0000-0000-0000-000000000000"
)

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

// sshRun runs cmd on the VM as the SSH user. It returns an error, never a
// panic, while the VM is rebooting.
func sshRun(vm testVM, cmd string) (string, error) {
	out, err := vm.machine.Command(cmd)
	return strings.TrimSpace(out), err
}

// bootID identifies the VM's current boot. It changes on every reboot, which
// is how the tier tells a node that rebooted from one that stayed up.
func bootID(vm testVM) (string, error) {
	return sshRun(vm, "cat /proc/sys/kernel/random/boot_id")
}

// writePhonehomeLogs writes AuroraBoot's log and each VM's logs into
// PHONEHOME_LOG_DIR, by default logs/phonehome under the e2e directory. It
// returns an error only when the log directory cannot be prepared; a log that
// cannot be fetched is printed and skipped.
func writePhonehomeLogs(vms []testVM) error {
	logDir := envOr("PHONEHOME_LOG_DIR", "logs/phonehome")
	logDir, err := filepath.Abs(logDir)
	if err != nil {
		return fmt.Errorf("resolving the log directory: %w", err)
	}
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return fmt.Errorf("creating the log directory: %w", err)
	}
	fmt.Fprintf(GinkgoWriter, "Writing the logs of the phonehome tier to %s\n", logDir)

	logs, _ := exec.Command("docker", "logs", webContainerName).CombinedOutput()
	if err := os.WriteFile(filepath.Join(logDir, "auroraboot.log"), logs, 0o644); err != nil {
		fmt.Fprintf(GinkgoWriter, "Could not write the AuroraBoot log: %s\n", err)
	}

	// vmLogDir reads VM_LOG_DIR, so it points there for the VM logs only.
	previous, had := os.LookupEnv("VM_LOG_DIR")
	if err := os.Setenv("VM_LOG_DIR", logDir); err != nil {
		return fmt.Errorf("setting VM_LOG_DIR: %w", err)
	}
	defer func() {
		if had {
			_ = os.Setenv("VM_LOG_DIR", previous)
		} else {
			_ = os.Unsetenv("VM_LOG_DIR")
		}
	}()
	for _, vm := range vms {
		saveSerialLog(vm)
		runForLog(vmLogSink(vm), "journalctl -u kairos-agent-phonehome --no-pager > /run/kairos-agent-phonehome.log")
		gatherLog(vmLogSink(vm), vmLogDir(vm), "/run/kairos-agent-phonehome.log")
		gatherLogs(vm)
	}
	return nil
}

var _ = Describe("phonehome on VMs", Label("phonehome-vm"), Ordered, Serial, func() {
	var (
		ws        *webServer
		cleanup   func()
		baseImage string
		groupID   string
		upgradeB  string
		token     string
		vms       []testVM
		nodeIDs   []string
		bootIDs   []string
		failed    bool
		setUp     bool
		artifact  []string
	)

	BeforeAll(func() {
		format.MaxLength = 0
		baseImage = envOr("PHONEHOME_BASE_IMAGE", phonehomeDefaultBaseImage)
		kairosInitImage := envOr("PHONEHOME_KAIROS_INIT_IMAGE", phonehomeDefaultKairosInitImage)

		// NewAuroraboot builds auroraboot:test from this checkout, so the
		// tier runs the code under test and never a stale local image.
		NewAuroraboot()
		ws, cleanup = startWebServer("auroraboot:test")
		groupID = ws.createGroup("e2e-phonehome")

		uid, err := uuid.NewV4()
		Expect(err).ToNot(HaveOccurred())
		token = uid.String()

		By("building the installed artifact and the upgrade target")
		bodyA := map[string]any{
			"name":            "e2e-phonehome-a",
			"baseImage":       baseImage,
			"arch":            "amd64",
			"kairosInitImage": kairosInitImage,
			"kairosVersion":   phonehomeKairosVersion,
			"outputs":         map[string]any{"iso": true},
			"provisioning": map[string]any{
				"autoInstall":        true,
				"registerAuroraBoot": true,
				"targetGroupId":      groupID,
			},
		}
		// The builder records the container image of every Ready artifact,
		// whatever outputs were asked for, so B needs no output to be served
		// by /image. Its Dockerfile builds a Kairos image, so AuroraBoot skips
		// kairos-init for it and the nodes run the base image's agent after
		// the upgrade.
		bodyB := map[string]any{
			"name":       "e2e-phonehome-b",
			"arch":       "amd64",
			"dockerfile": fmt.Sprintf("FROM %s\nRUN echo %s > %s\n", baseImage, token, phonehomeMarkerPath),
		}
		var installA string
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer GinkgoRecover()
			defer wg.Done()
			installA = ws.buildArtifact(bodyA)
		}()
		go func() {
			defer GinkgoRecover()
			defer wg.Done()
			upgradeB = ws.buildArtifact(bodyB)
		}()
		wg.Wait()
		for _, id := range []string{installA, upgradeB} {
			if id != "" {
				artifact = append(artifact, id)
			}
		}
		Expect(installA).ToNot(BeEmpty(), "the installed artifact did not build")
		Expect(upgradeB).ToNot(BeEmpty(), "the upgrade target did not build")

		iso := ws.downloadISO(installA, GinkgoT().TempDir())

		By("installing two VMs from the ISO")
		spec := vmSpec{ISO: iso, Memory: envOr("MEMORY", "4000"), CPUs: envOr("CPUS", "2")}
		for range 2 {
			vm, err := startVMWith(spec)
			// A VM that failed to start is still collected and destroyed,
			// so its serial log explains the failure and its state
			// directory goes away.
			vms = append(vms, vm)
			// Registered right after the start so an interrupt while the
			// logs are written cannot skip it. DeferCleanup runs after
			// AfterAll, which gathers the logs from the live VMs.
			DeferCleanup(func() {
				stopTPM(vm.StateDir)
				if err := vm.Destroy(nil); err != nil {
					fmt.Printf("Could not destroy VM %s: %s\n", vm.machine.Config().ID, err)
				}
			})
			Expect(err).ToNot(HaveOccurred())
		}
		// Booting the live ISO, installing, rebooting from disk and
		// registering share one 15-minute budget.
		installDeadline := time.Now().Add(15 * time.Minute)
		for _, vm := range vms {
			Eventually(func() error {
				_, err := sshRun(vm, "true")
				return err
			}, time.Until(installDeadline), 10*time.Second).Should(Succeed(),
				"VM %s accepting SSH after booting the ISO", vm.machine.Config().ID)
		}

		By("waiting for both nodes to register from disk in the group")
		// A VM can phone home from the live ISO before it installs. The group
		// has to hold exactly the two installed nodes, and they have to be
		// online on the installed system.
		var active []map[string]any
		Eventually(func(g Gomega) {
			active = ws.groupNodes(groupID)
			g.Expect(active).To(HaveLen(2))
			for _, n := range active {
				g.Expect(n).To(And(
					HaveKeyWithValue("bootState", "active"),
					HaveKeyWithValue("phase", "Online"),
				))
			}
		}, time.Until(installDeadline), 10*time.Second).Should(Succeed(),
			"exactly two nodes in group %s, registered with boot state active and online", groupID)

		for _, vm := range vms {
			var hostname string
			Eventually(func() error {
				var err error
				hostname, err = sshRun(vm, "hostname")
				return err
			}, "5m", "5s").Should(Succeed(), "reading the hostname of VM %s", vm.machine.Config().ID)
			var id string
			for _, n := range active {
				if n["hostname"] == hostname {
					id, _ = n["id"].(string)
				}
			}
			Expect(id).ToNot(BeEmpty(), "no active node has the hostname %q of VM %s: %v", hostname, vm.machine.Config().ID, active)
			nodeIDs = append(nodeIDs, id)

			boot, err := bootID(vm)
			Expect(err).ToNot(HaveOccurred(), boot)
			bootIDs = append(bootIDs, boot)
		}
		Expect(nodeIDs[0]).ToNot(Equal(nodeIDs[1]), "both VMs map to the same node")
		setUp = true
	})

	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			failed = true
		}
	})

	AfterAll(func() {
		// Log writing is best effort: a problem with the log directory is
		// reported and skips the logs, never the teardown below it.
		if failed || !setUp {
			if err := writePhonehomeLogs(vms); err != nil {
				fmt.Fprintf(GinkgoWriter, "Not writing the logs of the phonehome tier: %s\n", err)
			}
		}

		if ws != nil {
			for _, id := range artifact {
				ws.deleteArtifact(id)
			}
		}
		if cleanup != nil {
			cleanup()
		} else {
			// startWebServer hands out its cleanup only once the server is
			// healthy, so a container that never got there is removed here.
			_ = exec.Command("docker", "rm", "-f", webContainerName).Run()
		}
	})

	It("reports an upgrade that fails before the reboot and leaves the nodes usable", func() {
		ids := ws.batchCommandIDs(ws.sendGroupCommand(groupID, "upgrade",
			map[string]string{"source": "artifact:" + phonehomeUnknownArtifact}, nil))

		for i, nodeID := range nodeIDs {
			cmdID := ws.commandFor(nodeID, ids)
			Eventually(func() string { return ws.commandPhase(nodeID, cmdID) }, "10m", "5s").
				Should(BeElementOf("Completed", "Failed", "Expired", "Canceled"), "upgrade command %s of node %s finishing", cmdID, nodeID)
			cmd := ws.command(nodeID, cmdID)
			Expect(cmd.Phase).To(Equal("Failed"), "upgrade command of node %s: %s", nodeID, cmd.Result)

			Eventually(func() any { return ws.node(nodeID)["upgradeState"] }, "2m", "5s").
				Should(Equal("failed"), "upgrade state of node %s", nodeID)
			n := ws.node(nodeID)
			Expect(n["phase"]).To(Equal("Online"), "node %s: %v", nodeID, n)
			Expect(n["bootState"]).To(Equal("active"), "node %s: %v", nodeID, n)

			out, err := sshRun(vms[i], "true")
			Expect(err).ToNot(HaveOccurred(), "VM of node %s does not answer over SSH: %s", nodeID, out)
			boot, err := bootID(vms[i])
			Expect(err).ToNot(HaveOccurred(), boot)
			Expect(boot).To(Equal(bootIDs[i]), "node %s rebooted after a failed upgrade", nodeID)
		}
	})

	It("upgrades both nodes to the new artifact", func() {
		ids := ws.batchCommandIDs(ws.sendGroupCommand(groupID, "upgrade",
			map[string]string{"source": "artifact:" + upgradeB}, nil))

		for i, nodeID := range nodeIDs {
			cmdID := ws.commandFor(nodeID, ids)
			Eventually(func() string { return ws.commandPhase(nodeID, cmdID) }, "15m", "5s").
				Should(BeElementOf("Completed", "Failed", "Expired", "Canceled"), "upgrade command %s of node %s finishing", cmdID, nodeID)
			cmd := ws.command(nodeID, cmdID)
			Expect(cmd.Phase).To(Equal("Completed"), "upgrade command of node %s: %s", nodeID, cmd.Result)

			// A node that never rebooted stays online and active, so only a
			// new boot ID shows that it came back.
			Eventually(func() string {
				boot, err := bootID(vms[i])
				if err != nil {
					return bootIDs[i]
				}
				return boot
			}, "15m", "10s").ShouldNot(Equal(bootIDs[i]), "VM of node %s rebooting after the upgrade", nodeID)
		}

		for i, nodeID := range nodeIDs {
			Eventually(func() map[string]any { return ws.node(nodeID) }, "15m", "10s").Should(And(
				HaveKeyWithValue("upgradeState", "done"),
				HaveKeyWithValue("bootState", "active"),
				HaveKeyWithValue("phase", "Online"),
			), "node %s registering again after the upgrade", nodeID)

			marker, err := sshRun(vms[i], "cat "+phonehomeMarkerPath)
			Expect(err).ToNot(HaveOccurred(), marker)
			Expect(marker).To(Equal(token), "node %s does not run the upgrade target", nodeID)
		}

		logs, err := exec.Command("docker", "logs", webContainerName).CombinedOutput()
		Expect(err).ToNot(HaveOccurred(), string(logs))
		prefix := "image export for artifact " + upgradeB + ":"
		var exports []string
		for _, line := range strings.Split(string(logs), "\n") {
			if strings.Contains(line, prefix) {
				exports = append(exports, line)
			}
		}
		built := 0
		for _, line := range exports {
			switch {
			case strings.Contains(line, "built in"):
				built++
			case strings.Contains(line, "waited for the build of another request"),
				strings.Contains(line, "served from the cache"):
			default:
				Fail("unexpected image export line: " + line)
			}
		}
		Expect(built).To(Equal(1), "image exports of the upgrade target:\n%s", strings.Join(exports, "\n"))
	})

	It("upgrades the nodes one at a time", func() {
		uid, err := uuid.NewV4()
		Expect(err).ToNot(HaveOccurred())
		tokenC := uid.String()
		upgradeC := ws.buildArtifact(map[string]any{
			"name":       "e2e-phonehome-c",
			"arch":       "amd64",
			"dockerfile": fmt.Sprintf("FROM %s\nRUN echo %s > %s\n", baseImage, tokenC, phonehomeMarkerPath),
		})
		artifact = append(artifact, upgradeC)

		before := make([]string, len(vms))
		for i, vm := range vms {
			before[i], err = bootID(vm)
			Expect(err).ToNot(HaveOccurred(), before[i])
		}

		batchID := ws.sendGroupCommand(groupID, "upgrade",
			map[string]string{"source": "artifact:" + upgradeC}, map[string]any{"concurrency": 1})

		// Each node downloads, upgrades, reboots and phones home before the
		// next one starts, so the budget is per node.
		var b map[string]any
		Eventually(func() any {
			b = ws.batch(batchID)
			return b["phase"]
		}, time.Duration(len(nodeIDs))*15*time.Minute, "10s").
			Should(BeElementOf("completed", "failed", "stopped"), "batch %s finishing, 15 minutes per node", batchID)
		AddReportEntry("batch "+batchID, b)
		Expect(b["phase"]).To(Equal("completed"), "batch %s: %v", batchID, b)

		rows := batchNodes(b)
		Expect(rows).To(HaveLen(len(nodeIDs)), "batch %s: %v", batchID, b)
		for _, row := range rows {
			Expect(row["state"]).To(Equal("done"), "node row of batch %s: %v", batchID, row)
		}
		firstFinished, err := time.Parse(time.RFC3339Nano, fmt.Sprint(rows[0]["finishedAt"]))
		Expect(err).ToNot(HaveOccurred(), "finishedAt of the first node of batch %s: %v", batchID, rows[0])
		secondStarted, err := time.Parse(time.RFC3339Nano, fmt.Sprint(rows[1]["startedAt"]))
		Expect(err).ToNot(HaveOccurred(), "startedAt of the second node of batch %s: %v", batchID, rows[1])
		Expect(secondStarted.Before(firstFinished)).To(BeFalse(),
			"the second node of batch %s started at %s, before the first finished at %s", batchID, secondStarted, firstFinished)

		for i, nodeID := range nodeIDs {
			Eventually(func() string {
				boot, err := bootID(vms[i])
				if err != nil {
					return before[i]
				}
				return boot
			}, "5m", "10s").ShouldNot(Equal(before[i]), "VM of node %s rebooting after the upgrade", nodeID)

			marker, err := sshRun(vms[i], "cat "+phonehomeMarkerPath)
			Expect(err).ToNot(HaveOccurred(), marker)
			Expect(marker).To(Equal(tokenC), "node %s does not run the third artifact", nodeID)

			Eventually(func() any { return ws.node(nodeID)["upgradeState"] }, "2m", "5s").
				Should(Equal("done"), "upgrade state of node %s", nodeID)
		}
	})
})
