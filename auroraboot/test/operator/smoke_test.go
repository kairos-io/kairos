//go:build operator_e2e

package operator

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"

	buildv1alpha2 "github.com/kairos-io/kairos-operator/api/v1alpha2"
)

var _ = Describe("Operator smoke", func() {
	It("reaches Building phase after we submit a minimal OSArtifact", func() {
		ctx := context.Background()
		art := createOSArtifact(ctx, "auroraboot-smoke", minimalSpec())
		DeferCleanup(cleanupArtifact, ctx, art.Name)
		DeferCleanup(func() {
			if CurrentSpecReport().Failed() {
				collectDebugLogs(ctx, art.Name)
			}
		})
		waitForPhase(ctx, art.Name, buildv1alpha2.Building, 2*time.Minute)
	})
})
