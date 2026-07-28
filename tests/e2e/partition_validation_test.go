//go:build e2e

package e2e_test

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/spectrocloud/peg/matcher"
)

// oversizeError is the message Sanitize() returns when the requested manual
// partitions do not fit in the target disk. Sanitize runs before the image is
// pulled, so this surfaces without a successful pull having to happen first.
const oversizeError = "does not fit in the target disk"

// The e2e VM is created with a 20000MiB (~20GB) /dev/vda (see startVM's
// WithDriveSize). The default layout (state ~10GB, recovery ~6GB, oem, bios)
// already uses ~16.6GB, so tooBigConfig's 30000MiB persistent partition cannot
// possibly fit, while fittingConfig's 1024MiB persistent leaves clear headroom.
const (
	tooBigConfig = `#cloud-config
install:
  device: "/dev/vda"
  auto: true
  reboot: false
  poweroff: false
  partitions:
    persistent:
      size: 30000
users:
- name: kairos
  passwd: kairos
  groups:
    - admin
`

	fittingConfig = `#cloud-config
install:
  device: "/dev/vda"
  auto: true
  reboot: false
  poweroff: false
  partitions:
    persistent:
      size: 1024
users:
- name: kairos
  passwd: kairos
  groups:
    - admin
`
)

var _ = Describe("manual-install with custom partition sizes", Label("partition-validation"), func() {
	var vm VM

	BeforeEach(func() {
		vm = startVM()
		vm.EventuallyConnects(sshTimeout())
	})

	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			dumpSerial(vm)
		}
		if vm.StateDir != "" {
			_ = vm.Destroy(nil)
		}
	})

	// writeConfig drops the given cloud-config into the guest at /tmp/config.yaml.
	writeConfig := func(content string) {
		_, err := vm.Sudo(fmt.Sprintf("cat > /tmp/config.yaml <<'EOF'\n%s\nEOF", content))
		Expect(err).ToNot(HaveOccurred())
	}

	It("fails before pulling when the partitions exceed the disk size", func() {
		writeConfig(tooBigConfig)
		out, err := vm.Sudo(fmt.Sprintf(
			"kairos-agent manual-install --allow-insecure-registries --device /dev/vda --source %s /tmp/config.yaml",
			sourceURI()))
		Expect(err).To(HaveOccurred(), out)
		Expect(out).To(ContainSubstring(oversizeError), out)
		// Validation must short-circuit the install before the image is pulled,
		// so the post-pull marker must never appear.
		Expect(out).ToNot(ContainSubstring(postPullMarker), out)
	})

	It("gets past validation when the partitions fit the disk size", func() {
		writeConfig(fittingConfig)
		// With partitions that fit, the install proceeds past sanitization and
		// pulls/unpacks the image. We only assert it cleared the new check and
		// reached the pull stage; completing the whole install is out of scope.
		out, _ := vm.Sudo(fmt.Sprintf(
			"kairos-agent manual-install --allow-insecure-registries --device /dev/vda --source %s /tmp/config.yaml",
			sourceURI()))
		Expect(out).ToNot(ContainSubstring(oversizeError), out)
		Expect(out).To(ContainSubstring(postPullMarker), out)
	})
})
