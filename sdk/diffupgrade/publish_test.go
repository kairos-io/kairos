package diffupgrade_test

import (
	"net/http/httptest"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	ggcrregistry "github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/kairos/v4/sdk/diffupgrade"
	"github.com/kairos-io/kairos/v4/sdk/utils/image"
)

var _ = Describe("publishing diffs to an index", func() {
	var server *httptest.Server
	var ref name.Reference
	platform := &v1.Platform{OS: "linux", Architecture: "amd64"}

	BeforeEach(func() {
		server = httptest.NewServer(ggcrregistry.New())
		DeferCleanup(server.Close)
		var err error
		ref, err = name.ParseReference(strings.TrimPrefix(server.URL, "http://")+"/kairos/hadron:v3.diff", name.Insecure)
		Expect(err).ToNot(HaveOccurred())
	})

	diffImage := func() v1.Image {
		layer, err := random.Layer(64, "application/vnd.oci.image.layer.v1.tar")
		Expect(err).ToNot(HaveOccurred())
		img, err := mutate.AppendLayers(empty.Image, layer)
		Expect(err).ToNot(HaveOccurred())
		return img
	}

	push := func(base string) (v1.Hash, int) {
		img := diffImage()
		entries, err := diffupgrade.PushToIndex(ref, img, &diffupgrade.Manifest{Base: base, Target: "sha256:v3"}, platform)
		Expect(err).ToNot(HaveOccurred())
		digest, err := img.Digest()
		Expect(err).ToNot(HaveOccurred())
		return digest, entries
	}

	forBase := func(base string) func(map[string]string) bool {
		return func(a map[string]string) bool { return a[diffupgrade.BaseLabel] == base }
	}

	It("keeps one diff per base and lets a node pick its own", func() {
		_, entries := push("sha256:v1")
		Expect(entries).To(Equal(1))
		fromV2, entries := push("sha256:v2")
		Expect(entries).To(Equal(2))
		// Publishing from v1 again replaces its diff rather than adding one.
		fromV1, entries := push("sha256:v1")
		Expect(entries).To(Equal(2))

		idx, err := remote.Index(ref)
		Expect(err).ToNot(HaveOccurred())
		manifest, err := idx.IndexManifest()
		Expect(err).ToNot(HaveOccurred())
		Expect(manifest.Manifests).To(HaveLen(2))
		for _, d := range manifest.Manifests {
			Expect(d.Annotations).To(HaveKeyWithValue(diffupgrade.TargetLabel, "sha256:v3"))
			Expect(d.Platform).To(Equal(platform))
		}

		got, err := image.ResolveAnnotated(ref.String(), forBase("sha256:v1"), nil, nil, image.WithInsecureRegistry())
		Expect(err).ToNot(HaveOccurred())
		Expect(got).To(Equal(ref.Context().Digest(fromV1.String()).String()))

		got, err = image.ResolveAnnotated(ref.String(), forBase("sha256:v2"), nil, nil, image.WithInsecureRegistry())
		Expect(err).ToNot(HaveOccurred())
		Expect(got).To(Equal(ref.Context().Digest(fromV2.String()).String()))

		_, err = image.ResolveAnnotated(ref.String(), forBase("sha256:v0"), nil, nil, image.WithInsecureRegistry())
		Expect(err).To(MatchError(image.ErrNoMatchingManifest))

		By("finding each diff at its own tag too")
		for base, digest := range map[string]v1.Hash{"sha256:v1": fromV1, "sha256:v2": fromV2} {
			own, err := diffupgrade.BaseRefFor(ref.String(), base)
			Expect(err).ToNot(HaveOccurred())
			ownRef, err := name.ParseReference(own, name.Insecure)
			Expect(err).ToNot(HaveOccurred())
			img, err := remote.Image(ownRef)
			Expect(err).ToNot(HaveOccurred())
			Expect(img.Digest()).To(Equal(digest))
		}
	})

	It("replaces a single image pushed at the tag", func() {
		Expect(remote.Write(ref, diffImage())).To(Succeed())
		// A single image is left for the caller to check once pulled.
		got, err := image.ResolveAnnotated(ref.String(), forBase("sha256:v1"), nil, nil, image.WithInsecureRegistry())
		Expect(err).ToNot(HaveOccurred())
		Expect(got).To(Equal(ref.String()))

		_, entries := push("sha256:v1")
		Expect(entries).To(Equal(1))
		_, err = image.ResolveAnnotated(ref.String(), forBase("sha256:v1"), nil, nil, image.WithInsecureRegistry())
		Expect(err).ToNot(HaveOccurred())
	})
})
