package e2e_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/format"
)

// This suite drives the extensions feature through the AuroraBoot web server
// (REST API) and, when an agent image is supplied, through the phone-home
// command path on a live node container.
//
// Two tiers:
//
//   - "extensions-web"   — REST surface only (build, validate, bundle, delete,
//                          cloud-config bake). Self-contained: starts the
//                          auroraboot web container, talks HTTP. Always runs
//                          under its label.
//   - "extensions-agent" — full operator → node lifecycle (install/enable/
//                          disable/remove + compound upgrade + node_extensions
//                          tracking). Needs a Kairos image whose kairos-agent
//                          carries the `extension` phone-home command; supply
//                          it via EXTENSIONS_AGENT_IMAGE (CI pins a released
//                          image). Skipped when it is not set.
//
// The web container is started with --privileged (seccomp unconfined) because
// the Fedora-base mkfs.erofs used by `systemd-repart --make-ddi=sysext` is
// TSAN-instrumented and calls personality(ADDR_NO_RANDOMIZE), which the
// default Docker seccomp profile blocks.

const (
	webAdminPassword = "e2e-admin"
	webRegToken      = "e2e-reg"
	webContainerName = "auroraboot-e2e-web"
)

type webServer struct {
	baseURL string
	// port is the host port the server listens on, picked free at start
	// because the container shares the host network.
	port string
}

// startWebServer launches `auroraboot web` in a container on the host network
// and waits for /healthz. The returned cleanup removes the container.
func startWebServer(image string) (*webServer, func()) {
	_ = exec.Command("docker", "rm", "-f", webContainerName).Run()

	freePort, err := getFreePort()
	Expect(err).ToNot(HaveOccurred())
	webListenPort := strconv.Itoa(freePort)

	args := []string{
		"run", "-d", "--name", webContainerName,
		"--network", "host", "--privileged",
		// Bind the host's live /dev so loop device nodes the kernel creates
		// during `systemd-repart --make-ddi` (LOOP_CTL_GET_FREE) are visible
		// inside this long-lived container. Without it, a container started
		// before any /dev/loopN nodes existed snapshots an empty set and every
		// build dies with "device node /dev/loop0 is lost" — there is no udev
		// inside the container to materialise the node after creation.
		"-v", "/dev:/dev",
		"-v", "/var/run/docker.sock:/var/run/docker.sock",
		"-e", "AURORABOOT_ADMIN_PASSWORD=" + webAdminPassword,
		"-e", "AURORABOOT_REG_TOKEN=" + webRegToken,
		"-e", "AURORABOOT_URL=http://10.0.2.2:" + webListenPort,
		"--entrypoint", "auroraboot",
		image, "web", "--listen", ":" + webListenPort,
	}
	out, err := exec.Command("docker", args...).CombinedOutput()
	Expect(err).ToNot(HaveOccurred(), string(out))

	ws := &webServer{baseURL: "http://localhost:" + webListenPort, port: webListenPort}
	cleanup := func() {
		logs, _ := exec.Command("docker", "logs", "--tail", "40", webContainerName).CombinedOutput()
		if CurrentSpecReport().Failed() {
			AddReportEntry("auroraboot web logs", string(logs))
		}
		_ = exec.Command("docker", "rm", "-f", webContainerName).Run()
	}

	Eventually(func() int {
		resp, err := http.Get(ws.baseURL + "/healthz")
		if err != nil {
			return 0
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}, "60s", "2s").Should(Equal(http.StatusOK))

	return ws, cleanup
}

func (w *webServer) do(method, path string, body any) (*http.Response, []byte) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		Expect(err).ToNot(HaveOccurred())
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, w.baseURL+path, rdr)
	Expect(err).ToNot(HaveOccurred())
	req.Header.Set("Authorization", "Bearer "+webAdminPassword)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	Expect(err).ToNot(HaveOccurred())
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp, data
}

// buildExtension POSTs a build and blocks until it reaches a terminal phase.
// Returns the extension id and its final phase.
//
// systemd-repart --make-ddi allocates host loop devices, which under
// privileged containers occasionally races ("Failed to make loopback device:
// Device or resource busy"). That's a host-resource flake, not a logic bug, so
// we retry the build a couple of times when we see that specific signature.
func (w *webServer) buildExtension(body map[string]any) (string, string) {
	var id, phase, logs string
	for attempt := 1; attempt <= 4; attempt++ {
		id, phase, logs = w.buildOnce(body)
		if phase == "Ready" {
			return id, phase
		}
		if !isLoopbackRace(logs) {
			break
		}
		AddReportEntry(fmt.Sprintf("retrying build (attempt %d) after loopback race", attempt), id)
		// Give the host loop subsystem a moment to release devices before retrying.
		time.Sleep(time.Duration(attempt*3) * time.Second)
	}
	if phase != "Ready" {
		AddReportEntry("extension build logs ("+id+")", logs)
	}
	return id, phase
}

func (w *webServer) buildOnce(body map[string]any) (id, phase, logs string) {
	resp, data := w.do(http.MethodPost, "/api/v1/extensions", body)
	ExpectWithOffset(2, resp.StatusCode).To(Equal(http.StatusCreated), string(data))
	var created struct {
		ID string `json:"id"`
	}
	Expect(json.Unmarshal(data, &created)).To(Succeed())
	Expect(created.ID).ToNot(BeEmpty())
	id = created.ID

	Eventually(func() string {
		_, d := w.do(http.MethodGet, "/api/v1/extensions/"+id, nil)
		var rec struct {
			Phase string `json:"phase"`
		}
		_ = json.Unmarshal(d, &rec)
		phase = rec.Phase
		return phase
	}, "180s", "3s").Should(BeElementOf("Ready", "Error"))

	_, l := w.do(http.MethodGet, "/api/v1/extensions/"+id+"/logs", nil)
	logs = string(l)
	return id, phase, logs
}

func isLoopbackRace(logs string) bool {
	return strings.Contains(logs, "Device or resource busy") ||
		strings.Contains(logs, "make loopback device")
}

// Serial: builds run `systemd-repart --make-ddi` (host loop devices); keep this
// suite off the parallel path so it doesn't contend with the other loop-heavy
// suites. Ordered: specs share one web-server container started in BeforeAll.
var _ = Describe("extensions REST API", Label("extensions-web", "e2e"), Ordered, Serial, func() {
	var ws *webServer
	var cleanup func()

	BeforeAll(func() {
		format.MaxLength = 0
		NewAuroraboot() // ensure auroraboot:test is built
		ws, cleanup = startWebServer("auroraboot:test")
	})
	AfterAll(func() {
		if cleanup != nil {
			cleanup()
		}
	})

	Context("build", func() {
		It("builds a sysext from a container image", func() {
			id, phase := ws.buildExtension(map[string]any{
				"name": "web-sysext", "type": "sysext", "arch": "amd64", "version": "v0.1",
				"source": map[string]any{"mode": "image", "baseImage": "alpine:3.21"},
			})
			Expect(phase).To(Equal("Ready"))

			_, data := ws.do(http.MethodGet, "/api/v1/extensions/"+id, nil)
			var rec map[string]any
			Expect(json.Unmarshal(data, &rec)).To(Succeed())
			Expect(rec["rawFilename"]).To(Equal("web-sysext.sysext.raw"))

			// Download endpoint serves the raw bytes.
			resp, raw := ws.do(http.MethodGet,
				"/api/v1/extensions/"+id+"/download/web-sysext.sysext.raw", nil)
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			Expect(len(raw)).To(BeNumerically(">", 0))
		})

		It("builds a confext from a container image", func() {
			_, phase := ws.buildExtension(map[string]any{
				"name": "web-confext", "type": "confext", "arch": "amd64", "version": "v0.1",
				"source": map[string]any{"mode": "image", "baseImage": "alpine:3.21"},
			})
			Expect(phase).To(Equal("Ready"))
		})

		It("builds a sysext from a Dockerfile and stores its build log", func() {
			id, phase := ws.buildExtension(map[string]any{
				"name": "web-df", "type": "sysext", "arch": "amd64", "version": "v0.1",
				"source": map[string]any{
					"mode":       "dockerfile",
					"dockerfile": "FROM alpine:3.21\nRUN apk add --no-cache jq\n",
				},
			})
			Expect(phase).To(Equal("Ready"))

			// The builder ignores log append errors, so a broken append only
			// shows as a log missing the docker build output.
			resp, logs := ws.do(http.MethodGet, "/api/v1/extensions/"+id+"/logs", nil)
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			Expect(string(logs)).To(ContainSubstring("apk add"))
		})
	})

	Context("validation", func() {
		post := func(body map[string]any) (int, string) {
			resp, data := ws.do(http.MethodPost, "/api/v1/extensions", body)
			return resp.StatusCode, string(data)
		}
		base := func(extra map[string]any) map[string]any {
			b := map[string]any{
				"name": "v", "type": "sysext", "arch": "amd64", "version": "x",
				"source": map[string]any{"mode": "image", "baseImage": "alpine:3.21"},
			}
			for k, v := range extra {
				b[k] = v
			}
			return b
		}

		It("rejects a hierarchy without a leading slash", func() {
			code, body := post(base(map[string]any{"hierarchies": []string{"opt"}}))
			Expect(code).To(Equal(http.StatusBadRequest))
			Expect(body).To(ContainSubstring("must start with /"))
		})
		It("rejects a hierarchy containing ..", func() {
			code, body := post(base(map[string]any{"hierarchies": []string{"/opt/../etc"}}))
			Expect(code).To(Equal(http.StatusBadRequest))
			Expect(body).To(ContainSubstring(".."))
		})
		It("rejects /usr as an explicit hierarchy", func() {
			code, body := post(base(map[string]any{"hierarchies": []string{"/usr"}}))
			Expect(code).To(Equal(http.StatusBadRequest))
			Expect(body).To(ContainSubstring("/usr"))
		})
		It("rejects extraSteps that start with FROM", func() {
			code, body := post(map[string]any{
				"name": "v", "type": "sysext", "arch": "amd64", "version": "x",
				"source": map[string]any{"mode": "artifact", "artifactId": "nonexistent", "extraSteps": "FROM ubuntu:24.04"},
			})
			Expect(code).To(Equal(http.StatusBadRequest))
			Expect(body).To(ContainSubstring("FROM"))
		})
		It("rejects an unsupported arch", func() {
			code, body := post(map[string]any{
				"name": "v", "type": "sysext", "arch": "i386",
				"source": map[string]any{"mode": "image", "baseImage": "alpine:3.21"},
			})
			Expect(code).To(Equal(http.StatusBadRequest))
			Expect(body).To(ContainSubstring("arch"))
		})
		It("rejects an unsupported source mode", func() {
			code, body := post(map[string]any{
				"name": "v", "type": "sysext", "arch": "amd64",
				"source": map[string]any{"mode": "voodoo"},
			})
			Expect(code).To(Equal(http.StatusBadRequest))
			Expect(body).To(ContainSubstring("mode"))
		})
		It("normalizes hierarchies (trailing slash, dedup, sort)", func() {
			id, phase := ws.buildExtension(base(map[string]any{
				"name": "web-hier", "hierarchies": []string{"/srv/", "/opt", "/srv", "/opt/"},
			}))
			Expect(phase).To(Equal("Ready"))
			_, data := ws.do(http.MethodGet, "/api/v1/extensions/"+id, nil)
			var rec struct {
				Hierarchies []string `json:"hierarchies"`
			}
			Expect(json.Unmarshal(data, &rec)).To(Succeed())
			Expect(rec.Hierarchies).To(Equal([]string{"/opt", "/srv"}))
		})
	})

	Context("lifecycle + bundles", func() {
		It("renames via PATCH and rejects an empty rename", func() {
			id, _ := ws.buildExtension(map[string]any{
				"name": "web-rename", "type": "sysext", "arch": "amd64", "version": "v0.1",
				"source": map[string]any{"mode": "image", "baseImage": "alpine:3.21"},
			})
			resp, _ := ws.do(http.MethodPatch, "/api/v1/extensions/"+id, map[string]any{"name": "web-renamed"})
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			_, data := ws.do(http.MethodGet, "/api/v1/extensions/"+id, nil)
			Expect(string(data)).To(ContainSubstring(`"name":"web-renamed"`))

			resp, _ = ws.do(http.MethodPatch, "/api/v1/extensions/"+id, map[string]any{})
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
		})

		It("blocks deletion while bundled, allows it once unbundled", func() {
			// A Ready sysext to reference.
			extID, phase := ws.buildExtension(map[string]any{
				"name": "web-bundled", "type": "sysext", "arch": "amd64", "version": "v0.1",
				"source": map[string]any{"mode": "image", "baseImage": "alpine:3.21"},
			})
			Expect(phase).To(Equal("Ready"))

			// An amd64 artifact to attach the bundle to.
			artResp, artData := ws.do(http.MethodPost, "/api/v1/artifacts", map[string]any{
				"name": "web-artifact", "baseImage": "alpine:3.21", "arch": "amd64",
				"outputs":      map[string]any{"iso": false},
				"provisioning": map[string]any{"registerAuroraBoot": false},
			})
			Expect(artResp.StatusCode).To(Equal(http.StatusCreated), string(artData))
			var art struct {
				ID string `json:"id"`
			}
			Expect(json.Unmarshal(artData, &art)).To(Succeed())
			// We only need the record; cancel the heavy build.
			ws.do(http.MethodPost, "/api/v1/artifacts/"+art.ID+"/cancel", nil)

			// Attach + verify.
			resp, data := ws.do(http.MethodPut, "/api/v1/artifacts/"+art.ID+"/bundle-extensions",
				[]map[string]any{{"extensionName": "web-bundled", "extensionType": "sysext"}})
			Expect(resp.StatusCode).To(Equal(http.StatusOK), string(data))

			resp, _ = ws.do(http.MethodGet, "/api/v1/artifacts/"+art.ID+"/bundle-extensions", nil)
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			// Resolve.
			resp, data = ws.do(http.MethodPost, "/api/v1/artifacts/"+art.ID+"/bundle-resolve", nil)
			Expect(resp.StatusCode).To(Equal(http.StatusOK), string(data))
			Expect(string(data)).To(ContainSubstring("web-bundled"))
			Expect(string(data)).To(ContainSubstring("/download/"))
			// The resolved source goes into an upgrade command's `extensions`
			// arg, which the node fetches with no Authorization header. So it
			// carries the extension's own download token, and never the admin
			// password.
			_, extData := ws.do(http.MethodGet, "/api/v1/extensions/"+extID, nil)
			var extRec struct {
				DownloadToken string `json:"downloadToken"`
			}
			Expect(json.Unmarshal(extData, &extRec)).To(Succeed())
			Expect(extRec.DownloadToken).ToNot(BeEmpty(), string(extData))
			Expect(string(data)).To(ContainSubstring("?token=" + extRec.DownloadToken))
			Expect(string(data)).ToNot(ContainSubstring(webAdminPassword))

			// Delete blocked → 409.
			resp, data = ws.do(http.MethodDelete, "/api/v1/extensions/"+extID, nil)
			Expect(resp.StatusCode).To(Equal(http.StatusConflict), string(data))
			Expect(string(data)).To(ContainSubstring(art.ID))

			// Unbundle, then delete succeeds → 204.
			ws.do(http.MethodPut, "/api/v1/artifacts/"+art.ID+"/bundle-extensions", []map[string]any{})
			resp, _ = ws.do(http.MethodDelete, "/api/v1/extensions/"+extID, nil)
			Expect(resp.StatusCode).To(Equal(http.StatusNoContent))
		})
	})

	Context("artifact hierarchies bake", func() {
		It("bakes SYSTEMD_{SYSEXT,CONFEXT}_HIERARCHIES drop-ins and persists the field", func() {
			resp, data := ws.do(http.MethodPost, "/api/v1/artifacts", map[string]any{
				"name": "web-bake", "baseImage": "alpine:3.21", "arch": "amd64",
				"outputs":              map[string]any{"iso": false},
				"provisioning":         map[string]any{"registerAuroraBoot": false},
				"extensionHierarchies": map[string]any{"sysext": []string{"/opt", "/srv"}, "confext": []string{"/srv/configs"}},
			})
			Expect(resp.StatusCode).To(Equal(http.StatusCreated), string(data))
			var art struct {
				ID string `json:"id"`
			}
			Expect(json.Unmarshal(data, &art)).To(Succeed())
			ws.do(http.MethodPost, "/api/v1/artifacts/"+art.ID+"/cancel", nil)

			_, recData := ws.do(http.MethodGet, "/api/v1/artifacts/"+art.ID, nil)
			rec := string(recData)
			Expect(rec).To(ContainSubstring("SYSTEMD_SYSEXT_HIERARCHIES=/usr:/opt:/srv"))
			Expect(rec).To(ContainSubstring("SYSTEMD_CONFEXT_HIERARCHIES=/etc:/srv/configs"))
			// The field round-trips on the record (drives the builder cross-check).
			Expect(rec).To(ContainSubstring(`"sysext":["/opt","/srv"]`))
		})
	})
})

// ----------------------------------------------------------------------------
// Agent-flow tier — only runs when EXTENSIONS_AGENT_IMAGE points at a Kairos
// image whose kairos-agent has the `extension` phone-home command.
// ----------------------------------------------------------------------------

const (
	agentContainerName = "auroraboot-e2e-node"
	agentNodeDir       = "/tmp/auroraboot-e2e-node-state"
	agentOemDir        = "/tmp/auroraboot-e2e-node-oem"
)

// Serial: the BeforeAll build runs `systemd-repart --make-ddi` (host loop
// devices) and the tier drives a live node container; keep it off the parallel
// path. Ordered: register -> install -> remove share state across specs.
var _ = Describe("extensions agent flow", Label("extensions-agent", "e2e"), Ordered, Serial, func() {
	var ws *webServer
	var cleanup func()
	var agentImage string
	var sysextID string

	BeforeAll(func() {
		agentImage = os.Getenv("EXTENSIONS_AGENT_IMAGE")
		if agentImage == "" {
			Skip("set EXTENSIONS_AGENT_IMAGE to a Kairos image with the `extension` phone-home command to run the agent-flow tier")
		}
		format.MaxLength = 0
		NewAuroraboot()
		ws, cleanup = startWebServer("auroraboot:test")

		// Build a sysext to push.
		var phase string
		sysextID, phase = ws.buildExtension(map[string]any{
			"name": "agent-tools", "type": "sysext", "arch": "amd64", "version": "v0.1",
			"source": map[string]any{"mode": "image", "baseImage": "alpine:3.21"},
		})
		Expect(phase).To(Equal("Ready"))

		// Node cloud-config: opt the `extension` command into the allow list.
		Expect(os.MkdirAll(agentOemDir, 0o755)).To(Succeed())
		Expect(os.MkdirAll(agentNodeDir, 0o755)).To(Succeed())
		cc := "#cloud-config\nphonehome:\n  url: http://localhost:" + ws.port + "\n" +
			"  registration_token: " + webRegToken + "\n" +
			"  allowed_commands:\n    - extension\n    - upgrade\n    - upgrade-recovery\n"
		Expect(os.WriteFile(agentOemDir+"/01-phonehome.yaml", []byte(cc), 0o644)).To(Succeed())

		startNodeAgent(agentImage)
	})

	AfterAll(func() {
		_ = exec.Command("docker", "rm", "-f", agentContainerName).Run()
		_ = exec.Command("docker", "run", "--rm", "--user", "root",
			"-v", agentNodeDir+":/work", "alpine:3.21", "rm", "-rf", "/work").Run()
		os.RemoveAll(agentOemDir)
		os.RemoveAll(agentNodeDir)
		if cleanup != nil {
			cleanup()
		}
	})

	nodeID := func() string {
		_, data := ws.do(http.MethodGet, "/api/v1/nodes", nil)
		var nodes []struct {
			ID    string `json:"id"`
			Phase string `json:"phase"`
		}
		Expect(json.Unmarshal(data, &nodes)).To(Succeed())
		for _, n := range nodes {
			if n.Phase == "Online" {
				return n.ID
			}
		}
		Expect(nodes).ToNot(BeEmpty())
		return nodes[len(nodes)-1].ID
	}

	sendCmd := func(node string, args map[string]string) string {
		resp, data := ws.do(http.MethodPost, "/api/v1/nodes/"+node+"/commands",
			map[string]any{"command": "extension", "args": args})
		Expect(resp.StatusCode).To(BeNumerically("<", 300), string(data))
		var cmd struct {
			ID string `json:"id"`
		}
		Expect(json.Unmarshal(data, &cmd)).To(Succeed())
		return cmd.ID
	}

	waitCmd := func(node, cmdID string) (string, string) {
		var phase, result string
		Eventually(func() string {
			_, data := ws.do(http.MethodGet, "/api/v1/nodes/"+node+"/commands", nil)
			var cmds []struct {
				ID     string `json:"id"`
				Phase  string `json:"phase"`
				Result string `json:"result"`
			}
			_ = json.Unmarshal(data, &cmds)
			for _, c := range cmds {
				if c.ID == cmdID {
					phase, result = c.Phase, c.Result
				}
			}
			return phase
		}, "60s", "2s").Should(BeElementOf("Completed", "Failed"))
		return phase, result
	}

	It("registers the node and reaches Online", func() {
		Eventually(nodeID, "30s", "2s").ShouldNot(BeEmpty())
	})

	It("installs the sysext and records node_extensions", func() {
		node := nodeID()
		// The source URL is pushed to every node in the selector and kept in
		// the commands table, so it carries the extension's own download
		// token. The admin password must not appear in it -- that is what the
		// UI used to send.
		_, data := ws.do(http.MethodGet, "/api/v1/extensions/"+sysextID, nil)
		var rec struct {
			DownloadToken string `json:"downloadToken"`
		}
		Expect(json.Unmarshal(data, &rec)).To(Succeed())
		Expect(rec.DownloadToken).ToNot(BeEmpty(), string(data))
		Expect(rec.DownloadToken).ToNot(Equal(webAdminPassword))

		src := ws.baseURL + "/api/v1/extensions/" + sysextID +
			"/download/agent-tools.sysext.raw?token=" + rec.DownloadToken
		Expect(src).ToNot(ContainSubstring(webAdminPassword))
		cmdID := sendCmd(node, map[string]string{
			"type": "sysext", "action": "install", "name": "agent-tools",
			"source": src, "bootState": "common", "now": "false",
		})
		phase, result := waitCmd(node, cmdID)
		Expect(phase).To(Equal("Completed"), result)

		// .raw + scope symlink on the node.
		out, err := exec.Command("docker", "exec", agentContainerName, "sh", "-c",
			"ls /var/lib/kairos/extensions/agent-tools.sysext.raw /var/lib/kairos/extensions/common/agent-tools.sysext.raw").CombinedOutput()
		Expect(err).ToNot(HaveOccurred(), string(out))

		// node_extensions populated via the WS status callback.
		Eventually(func() string {
			_, data := ws.do(http.MethodGet, "/api/v1/nodes/"+node+"/extensions", nil)
			return string(data)
		}, "15s", "2s").Should(And(ContainSubstring("agent-tools"), ContainSubstring(`"bootState":"common"`)))
	})

	It("removes the sysext", func() {
		node := nodeID()
		cmdID := sendCmd(node, map[string]string{
			"type": "sysext", "action": "remove", "name": "agent-tools", "now": "false",
		})
		phase, result := waitCmd(node, cmdID)
		Expect(phase).To(Equal("Completed"), result)

		out, _ := exec.Command("docker", "exec", agentContainerName, "sh", "-c",
			"find /var/lib/kairos/extensions -type f -o -type l 2>/dev/null").CombinedOutput()
		Expect(strings.TrimSpace(string(out))).To(BeEmpty())
	})
})

// startNodeAgent runs the phone-home agent in a container on the host network,
// with the cloud-config under /oem and a writable persistent dir.
func startNodeAgent(image string) {
	_ = exec.Command("docker", "rm", "-f", agentContainerName).Run()
	args := []string{
		"run", "-d", "--name", agentContainerName,
		"--network", "host", "--privileged",
		"-v", agentOemDir + ":/oem",
		"-v", agentNodeDir + ":/var/lib/kairos",
		"--entrypoint", "/usr/bin/kairos-agent",
		image, "phone-home",
	}
	out, err := exec.Command("docker", args...).CombinedOutput()
	Expect(err).ToNot(HaveOccurred(), string(out))

	// Give it a moment to register.
	Eventually(func() string {
		o, _ := exec.Command("docker", "logs", agentContainerName).CombinedOutput()
		return string(o)
	}, "30s", "2s").Should(ContainSubstring("registered as node"),
		fmt.Sprintf("agent %s never registered", image))
	time.Sleep(1 * time.Second)
}
