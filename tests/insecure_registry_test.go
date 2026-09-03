package mos_test

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/spectrocloud/peg/matcher"
)

// insecureRegistryContainer is the docker container name for the plain-HTTP
// registry:2 the insecure-registry cell uses. Also reused as the sourceURI
// target for the partition-validation cell, which needs a reachable oci:
// source to exercise the pre-pull size guard even though it does not care
// about TLS.
const insecureRegistryContainer = "kairos-e2e-registry"

// insecureRegistryBaseImage returns the OCI image the local registry serves.
// Override with BASE_IMAGE. Default is a plain (not kairosified) Hadron base;
// the test only needs the image to be pullable, its contents do not have to
// boot.
func insecureRegistryBaseImage() string {
	if v := os.Getenv("BASE_IMAGE"); v != "" {
		return v
	}
	return "ghcr.io/kairos-io/hadron:v0.5.1"
}

var (
	insecureRegistryPort int
	insecureRegistryRepo string
)

// dockerCmd runs a docker command and returns combined output, wrapping
// errors with the output so failures are debuggable.
func dockerCmd(args ...string) (string, error) {
	out, err := exec.Command("docker", args...).CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("docker %s: %w: %s", strings.Join(args, " "), err, string(out))
	}
	return string(out), nil
}

// startInsecureRegistry pulls the base image, brings up a plain-HTTP
// registry:2 container on a free host port, and re-pushes the base image
// into it under kairos/source:test. Sets insecureRegistryPort and
// insecureRegistryRepo. Fails the current spec on any error.
func startInsecureRegistry() {
	baseImage := insecureRegistryBaseImage()
	out, err := dockerCmd("pull", baseImage)
	Expect(err).ToNot(HaveOccurred(), out)

	port := runRegistryContainer()

	// Wait for the registry HTTP listener to accept requests before pushing.
	// Without this, a `docker push` fired immediately after `docker run -d`
	// intermittently races the container's HTTP startup and returns
	// "connection reset by peer", failing every spec that depends on this
	// registry.
	waitForRegistryReady(port)

	repo := "kairos/source:test"
	localRef := fmt.Sprintf("localhost:%d/%s", port, repo)
	out, err = dockerCmd("tag", baseImage, localRef)
	Expect(err).ToNot(HaveOccurred(), out)

	out, err = dockerCmd("push", localRef)
	Expect(err).ToNot(HaveOccurred(), out)

	insecureRegistryPort = port
	insecureRegistryRepo = repo
}

// registryRunAttempts is how many times startInsecureRegistry tries to create
// the registry container before giving up.
const registryRunAttempts = 5

// runRegistryContainer creates the registry:2 container on a free host port and
// returns the port it was given.
//
// The run is retried because the daemon gives a new container's veth 200ms to
// reach the bridge's forwarding state, and a loaded runner does not always make
// it, failing with "check bridge port state: bridge port not forwarding after
// 200ms". That takes down the partition-validation cell as well, which shares
// this BeforeAll for its oci: source and does not otherwise care about
// registries.
//
// Both the container name and the port are reset between attempts:
//
//   - a run that fails during network setup has already created the container,
//     so the name stays taken and a plain retry fails on "container name is
//     already in use" instead;
//   - getFreePort closes its listener before returning, so a port is only free
//     at the instant it is picked. Reusing one across attempts re-runs that
//     race for nothing.
func runRegistryContainer() int {
	GinkgoHelper()

	var (
		port int
		out  string
		err  error
	)
	for attempt := 1; attempt <= registryRunAttempts; attempt++ {
		// Also clears a container left behind by an earlier run on this runner.
		_, _ = dockerCmd("rm", "-f", insecureRegistryContainer)

		port, err = getFreePort()
		if err != nil {
			out = err.Error()
			continue
		}

		out, err = dockerCmd("run", "-d", "--name", insecureRegistryContainer,
			"-p", fmt.Sprintf("%d:5000", port), "registry:2")
		if err == nil {
			return port
		}
		GinkgoWriter.Printf("registry container did not come up (attempt %d/%d): %s\n",
			attempt, registryRunAttempts, out)
		if attempt < registryRunAttempts {
			time.Sleep(2 * time.Second)
		}
	}
	Expect(err).ToNot(HaveOccurred(), "registry container did not come up in %d attempts: %s",
		registryRunAttempts, out)
	return port
}

// stopInsecureRegistry force-removes the registry container. Errors are
// swallowed at teardown.
func stopInsecureRegistry() {
	_, _ = dockerCmd("rm", "-f", insecureRegistryContainer)
}

// waitForRegistryReady polls the registry's /v2/ endpoint until it responds
// with a 2xx status. registry:2 backs /v2/ once its HTTP server is fully
// serving, so a successful GET is a reliable readiness marker. Fails the
// current spec if the endpoint does not come up within 60s.
func waitForRegistryReady(port int) {
	GinkgoHelper()
	url := fmt.Sprintf("http://localhost:%d/v2/", port)
	client := &http.Client{Timeout: 2 * time.Second}
	Eventually(func() error {
		resp, err := client.Get(url)
		if err != nil {
			return err
		}
		resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return nil
		}
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}, 60*time.Second, 500*time.Millisecond).Should(Succeed(),
		"registry:2 at %s did not become ready", url)
}

// insecureSourceURI is the oci: source the guest uses to reach the host
// registry. 10.0.2.2 is the QEMU user-net host gateway; .sslip.io makes
// go-containerregistry default to HTTPS (not RFC1918 auto-HTTP), so
// --allow-insecure-registries actually matters.
func insecureSourceURI() string {
	return fmt.Sprintf("oci:10.0.2.2.sslip.io:%d/%s", insecureRegistryPort, insecureRegistryRepo)
}

// insecureTLSError is the go-containerregistry failure when an HTTPS client
// hits a plain-HTTP registry.
const insecureTLSError = "server gave HTTP response to HTTPS client"

// insecurePostPullMarker is logged only after the source image has been
// pulled and unpacked into the target, so it never appears in the TLS
// failure path.
const insecurePostPullMarker = "Finished copying"

var _ = Describe("manual-install against an insecure registry", Label("insecure-registry"), Ordered, func() {
	var vm VM

	BeforeAll(func() {
		startInsecureRegistry()
	})

	AfterAll(func() {
		stopInsecureRegistry()
	})

	BeforeEach(func() {
		_, vm = startVM()
		vm.EventuallyConnects(1200)

		out, err := vm.Sudo(`cat > /tmp/config.yaml <<'EOF'
#cloud-config
install:
  device: "/dev/vda"
  auto: true
  reboot: false
  poweroff: false
users:
- name: kairos
  passwd: kairos
  groups:
    - admin
EOF`)
		Expect(err).ToNot(HaveOccurred(), out)
	})

	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			gatherLogs(vm)
		}
		Expect(vm.Destroy(nil)).ToNot(HaveOccurred())
	})

	It("fails at the pull without --allow-insecure-registries", func() {
		out, err := vm.Sudo(fmt.Sprintf(
			"kairos-agent manual-install --device /dev/vda --source %s /tmp/config.yaml",
			insecureSourceURI()))
		Expect(err).To(HaveOccurred(), out)
		Expect(out).To(ContainSubstring(insecureTLSError), out)
	})

	It("gets past the pull with --allow-insecure-registries", func() {
		// We only verify the pull stage: with the flag set the image is
		// pulled and unpacked successfully. The install may not run to
		// completion in this minimal setup, so the command error is
		// intentionally ignored.
		out, _ := vm.Sudo(fmt.Sprintf(
			"kairos-agent manual-install --allow-insecure-registries --device /dev/vda --source %s /tmp/config.yaml",
			insecureSourceURI()))
		Expect(out).ToNot(ContainSubstring(insecureTLSError), out)
		Expect(out).To(ContainSubstring(insecurePostPullMarker), out)
	})
})
