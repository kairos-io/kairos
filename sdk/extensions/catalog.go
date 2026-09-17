// Package extensions parses and resolves published system extension catalogs.
package extensions

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"
)

var immutableOCIReference = regexp.MustCompile(`^[^@[:space:]]+@sha256:[0-9a-fA-F]{64}$`)

// Catalog describes the extension layers published by a repository.
type Catalog struct {
	Repository string  `json:"repo"`
	Layers     []Layer `json:"layers"`
}

// Layer describes the available versions of one extension layer.
type Layer struct {
	Name   string `json:"name"`
	Latest string `json:"latest"`
	Tags   []Tag  `json:"tags"`
}

// Tag describes the artifacts published for one layer version.
type Tag struct {
	Version string              `json:"tag"`
	Sysext  map[string]Artifact `json:"sysext"`
}

// Artifact identifies a published system extension image.
type Artifact struct {
	OCI string `json:"oci"`
}

// Resolved identifies one immutable system extension artifact.
type Resolved struct {
	Repository   string
	Name         string
	Version      string
	Architecture string
	OCI          string
}

// Parse reads a catalog JSON document.
func Parse(reader io.Reader) (Catalog, error) {
	var catalog Catalog
	if err := json.NewDecoder(reader).Decode(&catalog); err != nil {
		return Catalog{}, fmt.Errorf("parse extension catalog: %w", err)
	}
	return catalog, nil
}

// Resolve finds an immutable system extension artifact.
//
// An empty version means the newest version that publishes an extension image
// for this architecture, which is the layer's own `latest` whenever that one
// does. A version that matches a published tag exactly selects that tag, so
// tags that are not valid semver still work. Anything else is read as a semver
// constraint (see
// https://github.com/Masterminds/semver#checking-version-constraints) and the
// highest published version that both satisfies it and publishes for this
// architecture wins.
//
// An index lists a layer's image tags whether or not an extension image was
// built for them: the tags come from the layer's own container package and the
// artifacts from a separate one. A tag with no artifact is therefore normal,
// and it must not hide the versions that do have one.
func (catalog Catalog) Resolve(name, version, architecture string) (Resolved, error) {
	for _, layer := range catalog.Layers {
		if layer.Name != name {
			continue
		}

		tag, err := layer.selectTag(version, architecture)
		if err != nil {
			return Resolved{}, err
		}
		artifact, found := tag.Sysext[architecture]
		if !found {
			published := tag.architectures()
			if len(published) == 0 {
				return Resolved{}, fmt.Errorf("layer %q version %q publishes no system extension image for any architecture", name, tag.Version)
			}
			return Resolved{}, fmt.Errorf("architecture %q is not available for layer %q version %q, which publishes %s",
				architecture, name, tag.Version, strings.Join(published, ", "))
		}
		if !immutableOCIReference.MatchString(artifact.OCI) {
			return Resolved{}, fmt.Errorf("OCI reference for layer %q version %q architecture %q must end with @sha256: and 64 hexadecimal characters", name, tag.Version, architecture)
		}

		return Resolved{
			Repository:   catalog.Repository,
			Name:         name,
			Version:      tag.Version,
			Architecture: architecture,
			OCI:          artifact.OCI,
		}, nil
	}

	return Resolved{}, fmt.Errorf("layer %q is not available", name)
}

// selectTag turns a requested version into the published tag to install.
//
// An explicit version that names a published tag is taken as it is, even when
// that tag publishes no image: the caller asked for that version, so saying
// the version is not installable is more useful than quietly installing a
// different one.
func (layer Layer) selectTag(requested, architecture string) (Tag, error) {
	if requested != "" {
		if tag, found := layer.tag(requested); found {
			return tag, nil
		}

		constraint, err := semver.NewConstraint(requested)
		if err != nil {
			// Not a published tag and not a constraint either: report the
			// request as the missing version rather than as a syntax error,
			// which is what it is from the caller's point of view.
			return Tag{}, fmt.Errorf("version %q is not available for layer %q", requested, layer.Name)
		}
		return layer.highestPublishing(architecture, constraint, requested)
	}

	if tag, found := layer.tag(layer.Latest); found && tag.publishes(architecture) {
		return tag, nil
	}
	return layer.highestPublishing(architecture, nil, "")
}

// highestPublishing picks the highest version that publishes an image for
// architecture, out of those satisfying constraint when there is one.
//
// Versions are ordered by semver. A layer whose tags are not semver at all
// keeps working: the first tag that publishes wins, and an index lists the
// newest tag first.
func (layer Layer) highestPublishing(architecture string, constraint *semver.Constraints, requested string) (Tag, error) {
	var (
		best      *semver.Version
		bestTag   Tag
		found     bool
		satisfied bool
	)
	for _, tag := range layer.Tags {
		candidate, err := semver.NewVersion(tag.Version)
		if constraint != nil {
			if err != nil || !constraint.Check(candidate) {
				continue
			}
			satisfied = true
		}
		if !tag.publishes(architecture) {
			continue
		}
		if err != nil {
			// Not semver, so it can only be taken in index order.
			if !found {
				bestTag, found = tag, true
			}
			continue
		}
		if best == nil || candidate.GreaterThan(best) {
			best, bestTag, found = candidate, tag, true
		}
	}
	if found {
		return bestTag, nil
	}

	published := layer.architectures()
	if constraint != nil {
		if !satisfied {
			return Tag{}, fmt.Errorf("no version of layer %q satisfies %q", layer.Name, requested)
		}
		if len(published) == 0 {
			return Tag{}, fmt.Errorf("no version of layer %q satisfying %q publishes a system extension image for any architecture", layer.Name, requested)
		}
		return Tag{}, fmt.Errorf("no version of layer %q satisfying %q publishes a system extension image for architecture %q; the layer publishes %s",
			layer.Name, requested, architecture, strings.Join(published, ", "))
	}
	if len(published) == 0 {
		return Tag{}, fmt.Errorf("layer %q publishes no system extension image for any architecture", layer.Name)
	}
	return Tag{}, fmt.Errorf("layer %q publishes no system extension image for architecture %q; it publishes %s",
		layer.Name, architecture, strings.Join(published, ", "))
}

// tag returns the published tag with this version.
func (layer Layer) tag(version string) (Tag, bool) {
	if version == "" {
		return Tag{}, false
	}
	for _, tag := range layer.Tags {
		if tag.Version == version {
			return tag, true
		}
	}
	return Tag{}, false
}

// architectures lists every architecture the layer publishes an image for, at
// any version.
func (layer Layer) architectures() []string {
	seen := map[string]struct{}{}
	for _, tag := range layer.Tags {
		for _, architecture := range tag.architectures() {
			seen[architecture] = struct{}{}
		}
	}
	return sorted(seen)
}

// architectures lists every architecture this version publishes an image for.
func (tag Tag) architectures() []string {
	seen := map[string]struct{}{}
	for architecture := range tag.Sysext {
		seen[architecture] = struct{}{}
	}
	return sorted(seen)
}

// publishes reports whether this version has an image for architecture. An
// entry that is present but malformed still counts, so a broken OCI reference
// is reported instead of being skipped over silently.
func (tag Tag) publishes(architecture string) bool {
	_, found := tag.Sysext[architecture]
	return found
}

func sorted(set map[string]struct{}) []string {
	list := make([]string, 0, len(set))
	for item := range set {
		list = append(list, item)
	}
	sort.Strings(list)
	return list
}

// Catalogs is an ordered list of catalogs searched as one. Order is
// significant: the first catalog that publishes a name wins, so a node can put
// its own index ahead of the default one and override what a name means.
type Catalogs []Catalog

// Resolve finds an artifact in the first catalog that publishes name.
//
// Catalogs later in the list that publish the same name are returned as
// shadowed, identified by their repository, so the caller can say what it
// ignored. When no catalog resolves the name, the error carries every
// catalog's reason.
func (list Catalogs) Resolve(name, version, architecture string) (Resolved, []string, error) {
	var (
		resolved Resolved
		found    bool
		shadowed []string
		reasons  []error
	)
	for _, catalog := range list {
		candidate, err := catalog.Resolve(name, version, architecture)
		if err != nil {
			reasons = append(reasons, fmt.Errorf("catalog %s: %w", catalog.identity(), err))
			continue
		}
		if found {
			shadowed = append(shadowed, candidate.Repository)
			continue
		}
		resolved, found = candidate, true
	}
	if !found {
		if len(reasons) == 0 {
			return Resolved{}, nil, fmt.Errorf("extension %q cannot be resolved: no catalog is configured", name)
		}
		return Resolved{}, nil, fmt.Errorf("extension %q cannot be resolved: %w", name, errors.Join(reasons...))
	}
	return resolved, shadowed, nil
}

// identity names a catalog in an error message. The index carries the
// repository it was built from, which is more useful than its position, but it
// is optional, so fall back to something that is never empty.
func (catalog Catalog) identity() string {
	if catalog.Repository != "" {
		return catalog.Repository
	}
	return "(unnamed)"
}
