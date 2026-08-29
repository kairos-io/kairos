package bundled_test

import (
	"strings"

	"github.com/kairos-io/kairos/v4/kairos-init/pkg/bundled"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Immucore dracut service", func() {
	It("finishes before switch-root can stop it", func() {
		lines := strings.Split(bundled.ImmucoreServiceDracut, "\n")

		Expect(lines).To(ContainElement("Before=initrd-switch-root.target"))
		Expect(lines).To(ContainElement("Conflicts=initrd-switch-root.target"))
	})
})
