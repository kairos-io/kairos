package mos_test

import (
	"context"

	. "github.com/spectrocloud/peg/matcher"

	. "github.com/onsi/ginkgo/v2"
)

var _ = Describe("kairos netboot test", Label("netboot-test"), func() {
	BeforeEach(func() {
		Machine.Create(context.Background())
	})
	AfterEach(func() {
		Machine.Clean()
	})

	It("eventually boots", func() {
		EventuallyConnects(1200)
	})
})
