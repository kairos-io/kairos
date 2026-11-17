package constants_test

import (
	"github.com/kairos-io/AuroraBoot/pkg/constants"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("GetXorrisoBooloaderArgs", Label("constants"), func() {
	var args []string

	BeforeEach(func() {
		args = constants.GetXorrisoBooloaderArgs("/tmp/root")
	})

	It("appends the ESP as EFI partition #2", func() {
		Expect(args).To(ContainElements("-append_partition", "2", "0xef"))
	})

	It("emits a GPT header alongside the protective MBR", func() {
		// Without appended_part_as=gpt, xorriso produces an MBR-only ISO
		// where the ESP is not exposed as a GPT partition. Strict UEFI
		// firmware (VMware ESXi, some OVMF/BMC stacks) refuses to boot
		// such ISOs because it enumerates ESPs via GPT only.
		Expect(pairedFlagValues(args, "-boot_image", "any")).To(
			ContainElement("appended_part_as=gpt"),
		)
	})

	It("keeps the protective MBR bootable for picky legacy BIOS firmware", func() {
		Expect(pairedFlagValues(args, "-boot_image", "any")).To(
			ContainElement("mbr_force_bootable=on"),
		)
	})

	It("keeps the isohybrid grub2 MBR wired up", func() {
		Expect(pairedFlagValues(args, "-boot_image", "grub")).To(
			ContainElement(MatchRegexp(`^grub2_mbr=/tmp/root/+boot/boot_hybrid\.img$`)),
		)
	})
})

// pairedFlagValues walks the args list and returns every value that follows a
// (flag, group) pair. xorriso arguments come as repeated "-boot_image <group>
// <k=v>" triples, so this lets tests assert on the k=v values for a given
// (flag, group) without caring about ordering.
func pairedFlagValues(args []string, flag, group string) []string {
	var values []string
	for i := 0; i+2 < len(args); i++ {
		if args[i] == flag && args[i+1] == group {
			values = append(values, args[i+2])
		}
	}
	return values
}
