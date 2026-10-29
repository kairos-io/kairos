package diffupgrade

import (
	"errors"
	"net/http"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

// PushToIndex publishes the diff img, whose manifest is m, into the image
// index at ref, and returns how many diffs the index holds afterwards.
//
// One target release can be reached from several bases, one diff each, and
// a node has to find the one for the release it runs without pulling the
// others. So the diffs of a target live together in one index, at the tag a
// node looks them up by (see RefFor), and each entry is annotated with
// BaseLabel and TargetLabel. A node reads the index, a few KB, and pulls only
// the entry whose base is its own.
//
// An entry already in the index for the same base is replaced, so publishing
// again from the same base updates the diff instead of adding a second one.
// A single image at ref, as an earlier version of this command pushed, is
// replaced by the index.
//
// The read and the write of the index are not atomic. Two publishers writing
// the same tag at once can lose one of the two entries; run them one after
// the other.
func PushToIndex(ref name.Reference, img v1.Image, m *Manifest, platform *v1.Platform, opts ...remote.Option) (int, error) {
	idx := mutate.IndexMediaType(empty.Index, types.OCIImageIndex)
	entries := 1

	existing, err := existingIndex(ref, opts...)
	if err != nil {
		return 0, err
	}
	if existing != nil {
		manifest, err := existing.IndexManifest()
		if err != nil {
			return 0, err
		}
		for _, desc := range manifest.Manifests {
			if desc.Annotations[BaseLabel] == m.Base {
				continue
			}
			child, err := existing.Image(desc.Digest)
			if err != nil {
				return 0, err
			}
			idx = mutate.AppendManifests(idx, mutate.IndexAddendum{
				Add:        child,
				Descriptor: v1.Descriptor{Annotations: desc.Annotations, Platform: desc.Platform},
			})
			entries++
		}
	}

	idx = mutate.AppendManifests(idx, mutate.IndexAddendum{
		Add: img,
		Descriptor: v1.Descriptor{
			Annotations: map[string]string{BaseLabel: m.Base, TargetLabel: m.Target},
			Platform:    platform,
		},
	})
	return entries, remote.WriteIndex(ref, idx, opts...)
}

// existingIndex returns the index at ref, or nil when there is nothing at
// ref, or only a single image.
func existingIndex(ref name.Reference, opts ...remote.Option) (v1.ImageIndex, error) {
	desc, err := remote.Get(ref, opts...)
	var terr *transport.Error
	if errors.As(err, &terr) && terr.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !desc.MediaType.IsIndex() {
		return nil, nil
	}
	return desc.ImageIndex()
}
