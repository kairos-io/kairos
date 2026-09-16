package state

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	sdkState "github.com/kairos-io/kairos/v4/sdk/state"
)

var _ = Describe("auditLogPersistExpected", func() {
	It("binds the audit log on the boots that have a persistent partition", func() {
		// In-RAM boots report Active (kairos-sdk forces it when kairos.ram is
		// on the cmdline) and keep COS_PERSISTENT on local disk, so they are
		// covered by the Active case.
		Expect(auditLogPersistExpected(sdkState.Active)).To(BeTrue())
		Expect(auditLogPersistExpected(sdkState.Passive)).To(BeTrue())
	})

	It("leaves the audit log alone where there is no persistent state", func() {
		// Recovery and autoreset boot without the persistent volume on
		// purpose: kairos-init's 00_rootfs.yaml sets no VOLUMES on those
		// branches, so the state target would resolve onto the read-only
		// recovery image. Live media has no persistent partition at all.
		Expect(auditLogPersistExpected(sdkState.Recovery)).To(BeFalse())
		Expect(auditLogPersistExpected(sdkState.AutoReset)).To(BeFalse())
		Expect(auditLogPersistExpected(sdkState.LiveCD)).To(BeFalse())
		Expect(auditLogPersistExpected(sdkState.Unknown)).To(BeFalse())
	})
})
