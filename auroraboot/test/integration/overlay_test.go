package integration_test

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/AuroraBoot/pkg/client"
)

// Overlays travel through the API by ID only: the SDK uploads files and gets
// a UUID back, a build names that UUID, and the server resolves it to a
// directory it owns. These specs drive the real router, auth and GORM store
// through pkg/client, with only the builder mocked.
var _ = Describe("Artifact overlays by ID", Ordered, func() {
	var (
		ctx        context.Context
		overlayID  string
		overlayDir string
		firstID    string
		cloneID    string
	)

	BeforeAll(func() {
		ctx = context.Background()
	})

	expectStatus := func(err error, status int) {
		GinkgoHelper()
		var apiErr *client.APIError
		Expect(errors.As(err, &apiErr)).To(BeTrue(), "expected an APIError, got %v", err)
		Expect(apiErr.StatusCode).To(Equal(status))
	}

	It("uploads an overlay and returns a UUID naming a directory under the artifacts dir", func() {
		var err error
		overlayID, err = adminClient.Artifacts.UploadOverlay(ctx,
			client.OverlayFile{Name: "motd", Content: strings.NewReader("hello from the overlay\n")})
		Expect(err).NotTo(HaveOccurred())

		parsed, err := uuid.Parse(overlayID)
		Expect(err).NotTo(HaveOccurred())
		Expect(parsed.String()).To(Equal(overlayID))

		overlayDir = filepath.Join(testArtifactsDir, "overlays", overlayID)
		Expect(filepath.Join(overlayDir, "motd")).To(BeARegularFile())
	})

	It("builds with the overlay and stores only its ID", func() {
		art, err := adminClient.Artifacts.Create(ctx, client.CreateArtifactRequest{
			BaseImage: "quay.io/kairos/ubuntu:24.04",
			Outputs:   client.ArtifactOutputs{ISO: true},
			OverlayID: overlayID,
		})
		Expect(err).NotTo(HaveOccurred())
		firstID = art.ID
		Expect(art.OverlayID).To(Equal(overlayID))
		Expect(art.Phase).To(BeEquivalentTo("Pending"))

		opts, ok := artifactBuilder.optsFor(firstID)
		Expect(ok).To(BeTrue())
		Expect(opts.OverlayRootfs).To(Equal(overlayDir))
		Expect(opts.OverlayID).To(Equal(overlayID))

		stored, err := adminClient.Artifacts.Get(ctx, firstID)
		Expect(err).NotTo(HaveOccurred())
		Expect(stored.OverlayID).To(Equal(overlayID))
	})

	It("lets a second build reuse the overlay, as a clone does", func() {
		art, err := adminClient.Artifacts.Create(ctx, client.CreateArtifactRequest{
			BaseImage: "quay.io/kairos/ubuntu:24.04",
			Outputs:   client.ArtifactOutputs{ISO: true},
			OverlayID: overlayID,
		})
		Expect(err).NotTo(HaveOccurred())
		cloneID = art.ID

		opts, ok := artifactBuilder.optsFor(cloneID)
		Expect(ok).To(BeTrue())
		Expect(opts.OverlayRootfs).To(Equal(overlayDir))
	})

	It("keeps the overlay while another artifact still references it", func() {
		Expect(adminClient.Artifacts.Delete(ctx, firstID)).To(Succeed())
		Expect(overlayDir).To(BeADirectory())
	})

	It("removes the overlay with the last artifact that references it", func() {
		Expect(adminClient.Artifacts.Delete(ctx, cloneID)).To(Succeed())
		_, err := os.Stat(overlayDir)
		Expect(os.IsNotExist(err)).To(BeTrue(), "overlay dir should be gone, stat returned %v", err)
	})

	It("refuses a build naming the deleted overlay", func() {
		_, err := adminClient.Artifacts.Create(ctx, client.CreateArtifactRequest{
			BaseImage: "quay.io/kairos/ubuntu:24.04",
			Outputs:   client.ArtifactOutputs{ISO: true},
			OverlayID: overlayID,
		})
		expectStatus(err, http.StatusBadRequest)
	})

	DescribeTable("refuses an overlayId that is not an uploaded overlay",
		func(id string) {
			_, err := adminClient.Artifacts.Create(ctx, client.CreateArtifactRequest{
				BaseImage: "quay.io/kairos/ubuntu:24.04",
				Outputs:   client.ArtifactOutputs{ISO: true},
				OverlayID: id,
			})
			expectStatus(err, http.StatusBadRequest)
		},
		Entry("a server path", "/etc"),
		Entry("a path climbing out of the overlays dir", "../../etc"),
		Entry("a UUID that was never uploaded", uuid.New().String()),
	)

	It("refuses an overlay upload without credentials", func() {
		_, err := client.New(testServerURL).Artifacts.UploadOverlay(ctx,
			client.OverlayFile{Name: "motd", Content: strings.NewReader("x")})
		expectStatus(err, http.StatusUnauthorized)
	})

	It("refuses an extension catalog that is not an http(s) URL", func() {
		resp := adminPost(testServerURL, "/api/v1/artifacts", testAdminPassword, map[string]interface{}{
			"baseImage":          "quay.io/kairos/ubuntu:24.04",
			"outputs":            map[string]bool{"iso": true},
			"extensions":         []string{"nvidia"},
			"extensionsCatalogs": []string{"/etc/passwd"},
		})
		Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
		Expect(readBody(resp)).To(ContainSubstring("extensionsCatalogs"))
	})
})
