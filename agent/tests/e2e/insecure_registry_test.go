//go:build e2e

package e2e_test

import (
	"fmt"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/spectrocloud/peg/matcher"
)

// Strings observed in a real manual-install run. tlsError is the go-containerregistry
// failure when an HTTPS client hits a plain-HTTP registry; postPullMarker is logged
// only after the source image has been pulled and unpacked into the target, so it
// never appears in the TLS-failure path.
const (
	tlsError       = "server gave HTTP response to HTTPS client"
	postPullMarker = "Finished copying"
)

var _ = Describe("manual-install against an insecure registry", Label("insecure-registry"), func() {
	var vm VM

	BeforeEach(func() {
		vm = startVM()
		vm.EventuallyConnects(sshTimeout())

		cfg, err := os.ReadFile("config.yaml")
		Expect(err).ToNot(HaveOccurred())
		_, err = vm.Sudo(fmt.Sprintf("cat > /tmp/config.yaml <<'EOF'\n%s\nEOF", string(cfg)))
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			dumpSerial(vm)
		}
		if vm.StateDir != "" {
			_ = vm.Destroy(nil)
		}
	})

	It("fails at the pull without --allow-insecure-registries", func() {
		out, err := vm.Sudo(fmt.Sprintf(
			"kairos-agent manual-install --device /dev/vda --source %s /tmp/config.yaml",
			sourceURI()))
		Expect(err).To(HaveOccurred(), out)
		Expect(out).To(ContainSubstring(tlsError), out)
	})

	It("gets past the pull with --allow-insecure-registries", func() {
		// We only verify the pull stage: with the flag the image is pulled and
		// unpacked successfully. The install may not run to completion in this
		// minimal setup, so the command error is intentionally ignored.
		out, _ := vm.Sudo(fmt.Sprintf(
			"kairos-agent manual-install --allow-insecure-registries --device /dev/vda --source %s /tmp/config.yaml",
			sourceURI()))
		Expect(out).ToNot(ContainSubstring(tlsError), out)
		Expect(out).To(ContainSubstring(postPullMarker), out)
	})
})
