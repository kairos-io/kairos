package utils

import (
	sdkConstants "github.com/kairos-io/kairos/v4/sdk/constants"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("kairos partition label scanning", func() {
	DescribeTable("kairosLabel classifies a partition filesystem label",
		func(label string, wantOem, wantPersistent bool) {
			oem, persistent := kairosLabel(label)
			Expect(oem).To(Equal(wantOem))
			Expect(persistent).To(Equal(wantPersistent))
		},
		Entry("plaintext OEM", sdkConstants.OEMLabel, true, false),
		Entry("plaintext persistent", sdkConstants.PersistentLabel, false, true),
		Entry("LUKS OEM container", sdkConstants.OEMLUKSLabel, true, false),
		Entry("LUKS persistent container", sdkConstants.PersistentLUKSLabel, false, true),
		Entry("someone else's partition", "DATA", false, false),
		Entry("an unlabelled partition", "", false, false),
	)

	It("Sees an encrypted install as having both partitions", func() {
		// The partitions of a trusted-boot install, as ghw reports them
		// before anything is unlocked: the LUKS containers carry the _LUKS
		// labels and the plaintext labels exist only inside them.
		oem, persistent := kairosPartitionsIn([]string{
			"EFI", "COS_STATE",
			sdkConstants.OEMLUKSLabel, sdkConstants.PersistentLUKSLabel,
		})
		Expect(oem).To(BeTrue())
		Expect(persistent).To(BeTrue())
	})

	It("Counts every label that exempts a disk from the wipe guard", func() {
		// kairos-io/kairos#4798. The wipe guard waives operator consent for
		// a disk that already carries one of our labels, because appending
		// the missing sibling is the expected recovery path. The in-RAM step
		// then writes a fresh GPT whenever it believes BOTH partitions are
		// missing. So any label that exempts a disk but does not count as
		// present hands that disk to init_disk with nobody left to object,
		// and the operator's encrypted data goes with it.
		for _, label := range []string{
			sdkConstants.OEMLabel, sdkConstants.OEMLUKSLabel,
			sdkConstants.PersistentLabel, sdkConstants.PersistentLUKSLabel,
		} {
			oem, persistent := kairosLabel(label)
			Expect(oem || persistent).To(BeTrue(),
				"%s exempts a disk from the wipe guard but does not count as a present partition", label)
		}
	})

	It("Leaves a disk that is not ours alone", func() {
		oem, persistent := kairosPartitionsIn([]string{"EFI", "rootfs", "swap"})
		Expect(oem).To(BeFalse())
		Expect(persistent).To(BeFalse())
	})
})
