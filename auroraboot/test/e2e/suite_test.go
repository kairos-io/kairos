package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	diskfs "github.com/diskfs/go-diskfs"
	"github.com/diskfs/go-diskfs/disk"
	"github.com/diskfs/go-diskfs/filesystem"
	"github.com/diskfs/go-diskfs/filesystem/iso9660"
	"github.com/google/uuid"
	process "github.com/mudler/go-processmanager"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/spectrocloud/peg/matcher"
	"github.com/spectrocloud/peg/pkg/machine"
	"github.com/spectrocloud/peg/pkg/machine/types"

	"github.com/kairos-io/AuroraBoot/internal/builder/auroraboot"
	gormstore "github.com/kairos-io/AuroraBoot/internal/store/gorm"
	"github.com/kairos-io/AuroraBoot/pkg/server"
	"github.com/kairos-io/AuroraBoot/pkg/ws"
)

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "AuroraBoot E2E Suite")
}

var (
	// Server
	aurorabootURL string
	adminPass     = "e2e-admin"
	regToken      = "e2e-token"

	// Paths
	tmpDir       string
	artifactsDir string

	// Built ISO paths
	callHomeISO string // ISO with auroraboot config (auto-registers)
	vanillaISO  string // ISO without auroraboot config (for import test)

	// VM-accessible URL (host reachable from QEMU via 10.0.2.2)
	vmAuroraBootURL string

	// Config from env
	kairosBaseImage string
	agentRepo       string
	agentBranch     string
	prebuiltISO     string // optional: skip build, use this ISO directly
)

var _ = BeforeSuite(func() {
	var err error

	// Read config from environment
	kairosBaseImage = os.Getenv("KAIROS_BASE_IMAGE")
	agentRepo = os.Getenv("KAIROS_AGENT_REPO")
	agentBranch = os.Getenv("KAIROS_AGENT_BRANCH")
	prebuiltISO = os.Getenv("ISO")

	if kairosBaseImage == "" && prebuiltISO == "" {
		Skip("Set KAIROS_BASE_IMAGE or ISO env var to run e2e tests")
	}

	// Create temp directories
	tmpDir, err = os.MkdirTemp("", "auroraboot-e2e-*")
	Expect(err).ToNot(HaveOccurred())
	artifactsDir = filepath.Join(tmpDir, "artifacts")
	Expect(os.MkdirAll(artifactsDir, 0755)).To(Succeed())

	// Start auroraboot server
	store, err := gormstore.New(filepath.Join(tmpDir, "e2e.db"))
	Expect(err).ToNot(HaveOccurred())

	nodeStore := &gormstore.NodeStoreAdapter{S: store}
	commandStore := &gormstore.CommandStoreAdapter{S: store}
	groupStore := &gormstore.GroupStoreAdapter{S: store}
	artifactStore := &gormstore.ArtifactStoreAdapter{S: store}
	hub := ws.NewHub()

	// Pick a free port and compute URLs before creating the server
	// (so AuroraBootURL is available for the install script handler)
	serverPort, err := getFreePort()
	Expect(err).ToNot(HaveOccurred())
	aurorabootURL = fmt.Sprintf("http://127.0.0.1:%d", serverPort)
	vmAuroraBootURL = fmt.Sprintf("http://10.0.2.2:%d", serverPort)

	builder := auroraboot.New(artifactsDir, nil, artifactStore)

	e := server.New(server.Config{
		NodeStore:     nodeStore,
		CommandStore:  commandStore,
		GroupStore:    groupStore,
		ArtifactStore: artifactStore,
		Builder:       builder,
		AdminPassword: adminPass,
		RegToken:      regToken,
		AuroraBootURL: aurorabootURL,
		ArtifactsDir:  artifactsDir,
		Hub:           hub,
	})

	// Start server on the chosen port
	go func() {
		_ = e.Start(fmt.Sprintf(":%d", serverPort))
	}()
	// Wait for server to be ready
	Eventually(func() error {
		resp, err := http.Get(aurorabootURL + "/api/v1/install-agent")
		if err != nil {
			return err
		}
		resp.Body.Close()
		return nil
	}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

	GinkgoWriter.Printf("AuroraBoot running at %s\n", aurorabootURL)
	GinkgoWriter.Printf("VM-accessible auroraboot URL: %s\n", vmAuroraBootURL)

	// Create e2e-test group
	resp := adminPost("/api/v1/groups", map[string]string{
		"name": "e2e-test", "description": "E2E test group",
	})
	Expect(resp.StatusCode).To(Equal(http.StatusCreated))
	resp.Body.Close()

	// Build or use pre-built ISOs
	if prebuiltISO != "" {
		// Use pre-built ISO directly (for both call-home and vanilla)
		callHomeISO = prebuiltISO
		vanillaISO = prebuiltISO
		GinkgoWriter.Printf("Using pre-built ISO: %s\n", prebuiltISO)
	} else {
		buildISOs()
	}
})

var _ = AfterSuite(func() {
	if tmpDir != "" {
		// Collect logs before cleanup
		logDir := filepath.Join(tmpDir, "logs")
		if entries, err := os.ReadDir(logDir); err == nil {
			for _, e := range entries {
				if data, err := os.ReadFile(filepath.Join(logDir, e.Name())); err == nil {
					GinkgoWriter.Printf("=== %s ===\n%s\n", e.Name(), string(data))
				}
			}
		}
		os.RemoveAll(tmpDir)
	}
})

// buildCustomImage creates a Docker image from the base image with custom agent binary.
func buildCustomImage(overlayDir string) string {
	imageName := "auroraboot-e2e-custom:latest"

	// Create a Dockerfile that copies our custom agent on top of the base image
	dockerfileContent := fmt.Sprintf(`FROM %s
COPY kairos-agent /usr/bin/kairos-agent
RUN chmod +x /usr/bin/kairos-agent
`, kairosBaseImage)

	dockerfilePath := filepath.Join(tmpDir, "Dockerfile.custom")
	Expect(os.WriteFile(dockerfilePath, []byte(dockerfileContent), 0644)).To(Succeed())

	// Copy the agent binary next to the Dockerfile (build context)
	agentSrc := filepath.Join(overlayDir, "usr/bin/kairos-agent")
	agentDst := filepath.Join(tmpDir, "kairos-agent")
	// Agent may already be at tmpDir from the build step
	if _, err := os.Stat(agentDst); err != nil {
		copyFile(agentSrc, agentDst)
	}

	GinkgoWriter.Printf("Building custom image: docker build -t %s -f %s %s\n", imageName, dockerfilePath, tmpDir)

	cmd := exec.Command("docker", "build", "-t", imageName, "-f", dockerfilePath, tmpDir)
	cmd.Stdout = GinkgoWriter
	cmd.Stderr = GinkgoWriter
	err := cmd.Run()
	Expect(err).ToNot(HaveOccurred(), "Failed to build custom Docker image")

	return "docker:" + imageName
}

// buildISOs builds the call-home and vanilla ISOs using AuroraBoot.
// If KAIROS_AGENT_REPO is set, injects custom agent via overlay.
func buildISOs() {
	GinkgoWriter.Println("Building ISOs via AuroraBoot...")

	var overlayDir string

	// Optionally build custom kairos-agent
	if agentRepo != "" {
		GinkgoWriter.Printf("Building custom kairos-agent from %s", agentRepo)
		if agentBranch != "" {
			GinkgoWriter.Printf(" (branch: %s)", agentBranch)
			cmd := exec.Command("git", "-C", agentRepo, "checkout", agentBranch)
			out, err := cmd.CombinedOutput()
			Expect(err).ToNot(HaveOccurred(), string(out))
		}
		GinkgoWriter.Println()

		agentBinary := filepath.Join(tmpDir, "kairos-agent")
		cmd := exec.Command("go", "build", "-o", agentBinary, ".")
		cmd.Dir = agentRepo
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		out, err := cmd.CombinedOutput()
		Expect(err).ToNot(HaveOccurred(), string(out))

		overlayDir = filepath.Join(tmpDir, "overlay")
		Expect(os.MkdirAll(filepath.Join(overlayDir, "usr/bin"), 0755)).To(Succeed())
		copyFile(agentBinary, filepath.Join(overlayDir, "usr/bin/kairos-agent"))
		Expect(os.Chmod(filepath.Join(overlayDir, "usr/bin/kairos-agent"), 0755)).To(Succeed())
	}

	// If we have a custom agent, build a custom container image first,
	// then use that to build the ISO. The overlay-rootfs only works for live boot;
	// the installed system uses the container image content.
	effectiveImage := kairosBaseImage
	if overlayDir != "" {
		effectiveImage = buildCustomImage(overlayDir)
		GinkgoWriter.Printf("Custom image built: %s\n", effectiveImage)
	}

	// Build ONE ISO from the (possibly customized) image.
	// Cloud-config will be provided per-VM via datasource ISO.
	savedBaseImage := kairosBaseImage
	kairosBaseImage = effectiveImage
	baseISO := buildISO("base", "", "")
	kairosBaseImage = savedBaseImage
	callHomeISO = baseISO
	vanillaISO = baseISO
	GinkgoWriter.Printf("Base ISO built: %s\n", baseISO)
}

// buildISO creates a Kairos ISO using AuroraBoot via Docker (needs root for rootfs unpacking).
func buildISO(name, cloudConfig, overlayDir string) string {
	outputDir := filepath.Join(artifactsDir, name)
	Expect(os.MkdirAll(outputDir, 0755)).To(Succeed())

	auroraImage := os.Getenv("AURORA_IMAGE")
	if auroraImage == "" {
		auroraImage = "quay.io/kairos/auroraboot:latest"
	}

	// Use Docker-based auroraboot (needs --privileged for rootfs lchown)
	args := []string{
		"run", "--privileged",
		"-v", "/var/run/docker.sock:/var/run/docker.sock",
		"-v", outputDir + ":/output",
	}

	if cloudConfig != "" {
		ccFile := filepath.Join(tmpDir, name+"-cloud-config.yaml")
		Expect(os.WriteFile(ccFile, []byte(cloudConfig), 0644)).To(Succeed())
		args = append(args, "-v", ccFile+":/cloud-config.yaml")
	}

	if overlayDir != "" {
		args = append(args, "-v", overlayDir+":/overlay")
	}

	args = append(args, auroraImage,
		"build-iso",
		"--output", "/output",
		"--date=false",
	)

	if cloudConfig != "" {
		args = append(args, "--cloud-config", "/cloud-config.yaml")
	}

	if overlayDir != "" {
		args = append(args, "--overlay-rootfs", "/overlay")
	}

	// Handle image reference: if it starts with "docker:", use docker daemon directly
	args = append(args, kairosBaseImage)

	GinkgoWriter.Printf("Running: docker %s\n", strings.Join(args, " "))

	cmd := exec.Command("docker", args...)
	cmd.Stdout = GinkgoWriter
	cmd.Stderr = GinkgoWriter
	err := cmd.Run()
	Expect(err).ToNot(HaveOccurred())

	// Find the ISO file in output
	var isoPath string
	err = filepath.Walk(outputDir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if strings.HasSuffix(p, ".iso") {
			isoPath = p
		}
		return nil
	})
	Expect(err).ToNot(HaveOccurred())
	Expect(isoPath).ToNot(BeEmpty(), "No ISO found in "+outputDir)
	return isoPath
}

// --- VM helpers ---

func startVM(isoPath, stateDir, datasource string) VM {
	Expect(os.MkdirAll(stateDir, 0755)).To(Succeed())

	sshPort, err := getFreePort()
	Expect(err).ToNot(HaveOccurred())

	vmName := uuid.New().String()

	// Start TPM emulator
	emulateTPM(stateDir)

	opts := []types.MachineOption{
		types.QEMUEngine,
		types.WithISO(isoPath),
		types.WithMemory("4096"),
		types.WithCPU("2"),
		types.WithDriveSize("25000"),
		types.WithSSHPort(strconv.Itoa(sshPort)),
		types.WithID(vmName),
		types.WithSSHUser("kairos"),
		types.WithSSHPass("kairos"),
		types.WithStateDir(stateDir),
		types.WithDataSource(datasource),
		types.OnFailure(func(p *process.Process) {
			var serial string
			out, _ := os.ReadFile(p.StdoutPath())
			errBytes, _ := os.ReadFile(p.StderrPath())
			if serialBytes, err := os.ReadFile(path.Join(stateDir, "serial.log")); err == nil {
				serial = string(serialBytes)
			}
			GinkgoWriter.Printf("\nVM Failed.\nstdout: %s\nstderr: %s\nserial (last 500 chars): %s\n",
				string(out), string(errBytes), lastN(serial, 500))
		}),
		func(m *types.MachineConfig) error {
			// Serial logging
			m.Args = append(m.Args,
				"-chardev", fmt.Sprintf("stdio,mux=on,id=char0,logfile=%s,signal=off",
					path.Join(stateDir, "serial.log")),
				"-serial", "chardev:char0",
				"-mon", "chardev=char0",
			)
			// TPM
			m.Args = append(m.Args,
				"-chardev", fmt.Sprintf("socket,id=chrtpm,path=%s/swtpm-sock",
					path.Join(stateDir, "tpm")),
				"-tpmdev", "emulator,id=tpm0,chardev=chrtpm",
				"-device", "tpm-tis,tpmdev=tpm0",
			)
			// Boot order: disk first, then cdrom
			m.Args = append(m.Args, "-boot", "order=dc")
			// KVM
			m.Args = append(m.Args, "-enable-kvm")
			return nil
		},
	}

	m, err := machine.New(opts...)
	Expect(err).ToNot(HaveOccurred())

	vm := NewVM(m, stateDir)
	_, err = vm.Start(context.Background())
	Expect(err).ToNot(HaveOccurred())

	return vm
}

func emulateTPM(stateDir string) {
	t := path.Join(stateDir, "tpm")
	Expect(os.MkdirAll(t, os.ModePerm)).To(Succeed())

	cmd := exec.Command("swtpm",
		"socket",
		"--tpmstate", fmt.Sprintf("dir=%s", t),
		"--ctrl", fmt.Sprintf("type=unixio,path=%s/swtpm-sock", t),
		"--tpm2", "--log", "level=20")
	Expect(cmd.Start()).To(Succeed())

	Expect(os.WriteFile(
		path.Join(t, "pid"),
		[]byte(strconv.Itoa(cmd.Process.Pid)),
		0744,
	)).To(Succeed())
}

func getFreePort() (int, error) {
	a, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}
	l, err := net.ListenTCP("tcp", a)
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// CreateDatasource creates a cloud-init datasource ISO from YAML content.
func createDatasource(cloudConfig string) string {
	ds, err := os.MkdirTemp("", "datasource-*")
	Expect(err).ToNot(HaveOccurred())

	// Write user-data to temp file
	ccFile := filepath.Join(ds, "user-data.yaml")
	Expect(os.WriteFile(ccFile, []byte(cloudConfig), 0644)).To(Succeed())

	diskImg := filepath.Join(ds, "datasource.iso")
	var diskSize int64 = 1 * 1024 * 1024 // 1 MB
	mydisk, err := diskfs.Create(diskImg, diskSize, diskfs.SectorSizeDefault)
	Expect(err).ToNot(HaveOccurred())
	mydisk.LogicalBlocksize = 2048

	fspec := disk.FilesystemSpec{Partition: 0, FSType: filesystem.TypeISO9660, VolumeLabel: "cidata"}
	fs, err := mydisk.CreateFilesystem(fspec)
	Expect(err).ToNot(HaveOccurred())

	rw, err := fs.OpenFile("user-data", os.O_CREATE|os.O_RDWR)
	Expect(err).ToNot(HaveOccurred())
	_, err = rw.Write([]byte(cloudConfig))
	Expect(err).ToNot(HaveOccurred())
	Expect(rw.Close()).To(Succeed())

	rw, err = fs.OpenFile("meta-data", os.O_CREATE|os.O_RDWR)
	Expect(err).ToNot(HaveOccurred())
	_, err = rw.Write([]byte(""))
	Expect(err).ToNot(HaveOccurred())
	Expect(rw.Close()).To(Succeed())

	iso, ok := fs.(*iso9660.FileSystem)
	Expect(ok).To(BeTrue())
	Expect(iso.Finalize(iso9660.FinalizeOptions{RockRidge: true, VolumeIdentifier: "cidata"})).To(Succeed())

	return diskImg
}

// --- HTTP helpers ---

func adminGet(path string) *http.Response {
	req, err := http.NewRequest(http.MethodGet, aurorabootURL+path, nil)
	Expect(err).ToNot(HaveOccurred())
	req.Header.Set("Authorization", "Bearer "+adminPass)
	resp, err := http.DefaultClient.Do(req)
	Expect(err).ToNot(HaveOccurred())
	return resp
}

func adminPost(path string, body interface{}) *http.Response {
	data, err := json.Marshal(body)
	Expect(err).ToNot(HaveOccurred())
	req, err := http.NewRequest(http.MethodPost, aurorabootURL+path, bytes.NewReader(data))
	Expect(err).ToNot(HaveOccurred())
	req.Header.Set("Authorization", "Bearer "+adminPass)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	Expect(err).ToNot(HaveOccurred())
	return resp
}

func decodeJSON(resp *http.Response) map[string]interface{} {
	defer resp.Body.Close()
	var result map[string]interface{}
	Expect(json.NewDecoder(resp.Body).Decode(&result)).To(Succeed())
	return result
}

func decodeJSONArray(resp *http.Response) []map[string]interface{} {
	defer resp.Body.Close()
	var result []map[string]interface{}
	Expect(json.NewDecoder(resp.Body).Decode(&result)).To(Succeed())
	return result
}

func adminDelete(path string) *http.Response {
	req, err := http.NewRequest(http.MethodDelete, aurorabootURL+path, nil)
	Expect(err).ToNot(HaveOccurred())
	req.Header.Set("Authorization", "Bearer "+adminPass)
	resp, err := http.DefaultClient.Do(req)
	Expect(err).ToNot(HaveOccurred())
	return resp
}

func readBody(resp *http.Response) string {
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}

func copyFile(src, dst string) {
	data, err := os.ReadFile(src)
	Expect(err).ToNot(HaveOccurred())
	Expect(os.WriteFile(dst, data, 0755)).To(Succeed())
}

func lastN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

// Ensure process import is used
var _ = process.Process{}
