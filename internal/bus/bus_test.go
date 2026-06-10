package bus_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/kairos-installer/internal/bus"
)

var _ = Describe("Bus", func() {
	It("creates a fresh manager with no registered plugins", func() {
		b := bus.NewBus()
		Expect(b.HasRegisteredPlugins()).To(BeFalse())
	})
})
