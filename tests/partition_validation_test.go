package mos_test

import (
	"context"
	"fmt"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/spectrocloud/peg/matcher"
	"github.com/spectrocloud/peg/pkg/machine"
	"github.com/spectrocloud/peg/pkg/machine/types"
)

// oversizeError is the message kairos-agent's Sanitize() returns when the
// requested manual partitions do not fit in the target disk. Sanitize runs
// before the image is pulled, so this surfaces without a successful pull
// having to happen first.
const oversizeError = "does not fit in the target disk"

// The partition-validation VM is created with a 20000MiB (~20GB) /dev/vda
// so the tooBigConfig's 30000MiB persistent partition cannot fit while
// fittingConfig's 1024MiB has clear headroom. The monorepo default drive
// size (50GB in CI) would let both configs succeed and the negative case
// would silently stop testing what it claims.
const partitionValidationDriveSize = "20000"

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

var _ = Describe("manual-install with custom partition sizes", Label("partition-validation"), Ordered, func() {
	var vm VM

	BeforeAll(func() {
		startInsecureRegistry()
	})

	AfterAll(func() {
		stopInsecureRegistry()
	})

	BeforeEach(func() {
		stateDir, err := os.MkdirTemp("", "")
		Expect(err).ToNot(HaveOccurred())
		fmt.Printf("State dir: %s\n", stateDir)

		opts := defaultVMOptsNoDrives(stateDir)
		opts = append(opts, types.WithDriveSize(partitionValidationDriveSize))

		m, err := machine.New(opts...)
		Expect(err).ToNot(HaveOccurred())
		vm = NewVM(m, stateDir)
		_, err = vm.Start(context.Background())
		Expect(err).ToNot(HaveOccurred())

		vm.EventuallyConnects(1200)
	})

	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			gatherLogs(vm)
		}
		Expect(vm.Destroy(nil)).ToNot(HaveOccurred())
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
			insecureSourceURI()))
		Expect(err).To(HaveOccurred(), out)
		Expect(out).To(ContainSubstring(oversizeError), out)
		// Validation must short-circuit the install before the image is
		// pulled, so the post-pull marker must never appear.
		Expect(out).ToNot(ContainSubstring(insecurePostPullMarker), out)
	})

	It("gets past validation when the partitions fit the disk size", func() {
		writeConfig(fittingConfig)
		// With partitions that fit, the install proceeds past sanitization
		// and pulls/unpacks the image. We only assert it cleared the new
		// check and reached the pull stage; completing the whole install
		// is out of scope.
		out, _ := vm.Sudo(fmt.Sprintf(
			"kairos-agent manual-install --allow-insecure-registries --device /dev/vda --source %s /tmp/config.yaml",
			insecureSourceURI()))
		Expect(out).ToNot(ContainSubstring(oversizeError), out)
		Expect(out).To(ContainSubstring(insecurePostPullMarker), out)
	})
})
