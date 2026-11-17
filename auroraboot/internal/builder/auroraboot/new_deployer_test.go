package auroraboot_test

import (
	"context"
	"errors"
	"time"

	"github.com/kairos-io/AuroraBoot/internal/builder/auroraboot"
	"github.com/kairos-io/AuroraBoot/pkg/schema"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spectrocloud-labs/herd"
)

var _ = Describe("NewDeployer", func() {
	// start-netboot is a background step alone in the last layer. Without
	// CollectOrphans, Run returned before it did and CollectErrors read
	// nothing, so a build kept serving the previous artifact reported success.
	It("reports an error a background step returns after Run's last layer", func() {
		errRunning := errors.New("netboot already running")
		for i := 0; i < 50; i++ {
			d := auroraboot.NewDeployer(schema.Config{}, schema.ReleaseArtifact{})
			Expect(d.Add("first", herd.WithCallback(func(context.Context) error { return nil }))).To(Succeed())
			Expect(d.Add("background",
				herd.Background,
				herd.WithDeps("first"),
				herd.WithCallback(func(context.Context) error {
					time.Sleep(time.Millisecond)
					return errRunning
				}),
			)).To(Succeed())

			Expect(d.Run(context.Background())).To(Succeed())
			Expect(d.CollectErrors()).To(MatchError(ContainSubstring("netboot already running")))
		}
	})
})
