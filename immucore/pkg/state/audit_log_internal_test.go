package state

import (
	"bytes"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	internalUtils "github.com/kairos-io/kairos/v4/immucore/internal/utils"
	sdkState "github.com/kairos-io/kairos/v4/sdk/state"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
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
		//
		// kairos-io/kairos#4629 does ask for recovery coverage. It is not
		// delivered, and the doc comment of the function says what covering it
		// would take and why that is a decision for the maintainer.
		Expect(auditLogPersistExpected(sdkState.Recovery)).To(BeFalse())
		Expect(auditLogPersistExpected(sdkState.AutoReset)).To(BeFalse())
		Expect(auditLogPersistExpected(sdkState.LiveCD)).To(BeFalse())
		Expect(auditLogPersistExpected(sdkState.Unknown)).To(BeFalse())
	})
})

var _ = Describe("logAuditLogSkip", func() {
	var buf *bytes.Buffer
	var was logger.KairosLogger

	BeforeEach(func() {
		was = internalUtils.KLog
		buf = &bytes.Buffer{}
		internalUtils.KLog = logger.NewBufferLogger(buf)
		internalUtils.KLog.SetLevel("info")
	})

	AfterEach(func() {
		internalUtils.KLog = was
	})

	It("says on the boot log that a recovery boot has no audit log mount", func() {
		// The gap #4629 asks about has to be findable in the journal of the
		// machine, not only in the source of this package.
		logAuditLogSkip(sdkState.Recovery)

		Expect(buf.String()).To(ContainSubstring("no persistent volume"))
		Expect(buf.String()).To(ContainSubstring("recovery"))
		Expect(buf.String()).To(ContainSubstring("/var/log/audit"))
	})

	It("says the same for an autoreset boot", func() {
		logAuditLogSkip(sdkState.AutoReset)

		Expect(buf.String()).To(ContainSubstring("no persistent volume"))
	})

	It("keeps live media out of the boot log", func() {
		// Nothing to explain there: live media has no persistent partition,
		// and nobody looks for an audit trail on it.
		logAuditLogSkip(sdkState.LiveCD)

		Expect(buf.String()).To(BeEmpty())
	})
})
