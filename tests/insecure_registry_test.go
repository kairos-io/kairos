package mos_test

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

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
	port, err := getFreePort()
	Expect(err).ToNot(HaveOccurred())

	baseImage := insecureRegistryBaseImage()
	out, err := dockerCmd("pull", baseImage)
	Expect(err).ToNot(HaveOccurred(), out)

	out, err = dockerCmd("run", "-d", "--name", insecureRegistryContainer,
		"-p", fmt.Sprintf("%d:5000", port), "registry:2")
	Expect(err).ToNot(HaveOccurred(), out)

	repo := "kairos/source:test"
	localRef := fmt.Sprintf("localhost:%d/%s", port, repo)
	out, err = dockerCmd("tag", baseImage, localRef)
	Expect(err).ToNot(HaveOccurred(), out)

	out, err = dockerCmd("push", localRef)
	Expect(err).ToNot(HaveOccurred(), out)

	insecureRegistryPort = port
	insecureRegistryRepo = repo
}

// stopInsecureRegistry force-removes the registry container. Errors are
// swallowed at teardown.
func stopInsecureRegistry() {
	_, _ = dockerCmd("rm", "-f", insecureRegistryContainer)
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
