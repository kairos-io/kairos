package gorm_test

import (
	"math"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	gormstore "github.com/kairos-io/AuroraBoot/internal/store/gorm"
	"github.com/kairos-io/AuroraBoot/pkg/store"
)

var _ = Describe("Batch reboot timeout", func() {
	maxTimeout := time.Duration(store.MaxRebootTimeoutSeconds) * time.Second

	It("uses the default when the batch does not set one", func() {
		Expect(gormstore.RebootTimeout(0)).To(Equal(time.Duration(store.DefaultRebootTimeoutSeconds) * time.Second))
	})

	It("keeps a value at the maximum", func() {
		Expect(gormstore.RebootTimeout(store.MaxRebootTimeoutSeconds)).To(Equal(maxTimeout))
	})

	DescribeTable("caps a value above the maximum instead of overflowing",
		func(seconds int) {
			Expect(gormstore.RebootTimeout(seconds)).To(Equal(maxTimeout))
		},
		Entry("one past the maximum", store.MaxRebootTimeoutSeconds+1),
		Entry("past the time.Duration range", 10000000000),
		Entry("the largest int", math.MaxInt),
	)
})
