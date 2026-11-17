package builder_test

import (
	"context"

	"github.com/kairos-io/AuroraBoot/pkg/builder"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// noopExt is a minimal stub used only to confirm the interface compiles
// and matches the expected method set. Real implementations live in
// internal/builder/auroraboot.
type noopExt struct{}

func (noopExt) Build(context.Context, builder.ExtensionBuildOptions) (*builder.ExtensionBuildStatus, error) {
	return nil, nil
}
func (noopExt) Status(context.Context, string) (*builder.ExtensionBuildStatus, error) {
	return nil, nil
}
func (noopExt) List(context.Context) ([]*builder.ExtensionBuildStatus, error) { return nil, nil }
func (noopExt) Cancel(context.Context, string) error                          { return nil }

var _ = Describe("ExtensionBuilder interface", func() {
	It("compiles for a conformant implementation", func() {
		var _ builder.ExtensionBuilder = noopExt{}
		Expect(true).To(BeTrue())
	})

	It("exposes the phase constants reused from ArtifactBuilder", func() {
		Expect(builder.BuildPending).To(Equal("Pending"))
		Expect(builder.BuildBuilding).To(Equal("Building"))
		Expect(builder.BuildReady).To(Equal("Ready"))
		Expect(builder.BuildError).To(Equal("Error"))
	})
})
