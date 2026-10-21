package validation_test

import (
	"regexp"
	"strconv"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// oemDeviceRe matches a read of the OEM partition's block device from the
// agent state.
var oemDeviceRe = regexp.MustCompile(`state get oem\.name`)

var _ = Describe("Bundled cloudconfigs OEM partition", func() {
	// oem.name is the block device the OEM label sits on. When OEM is
	// encrypted, which every Trusted Boot install does, that device is the
	// LUKS container, and `mount` answers "unknown filesystem type
	// 'crypto_LUKS'". The unlocked partition is already mounted at /oem.
	//
	// 21_kcrypt.yaml used to do exactly that in after-upgrade, to refresh a
	// copy of the discovery plugins in /oem/system/discovery that install
	// stopped making in #683. Once UKI upgrades ran after-upgrade, every
	// upgrade of an encrypted Trusted Boot node failed on it.
	It("never reaches the OEM partition through its raw block device", func() {
		var offenders []string

		for name, lines := range cloudConfigLines() {
			for i, line := range lines {
				if oemDeviceRe.MatchString(line) {
					offenders = append(offenders, name+":"+strconv.Itoa(i+1)+" "+strings.TrimSpace(line))
				}
			}
		}

		Expect(offenders).To(BeEmpty(),
			"use the mounted /oem instead; the device is a LUKS container on an encrypted install")
	})
})
