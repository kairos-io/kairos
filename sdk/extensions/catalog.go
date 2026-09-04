// Package extensions parses and resolves published system extension catalogs.
package extensions

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"

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
// An empty version means the newest version the layer publishes. A version
// that matches a published tag exactly selects that tag, so tags that are not
// valid semver still work. Anything else is read as a semver constraint (see
// https://github.com/Masterminds/semver#checking-version-constraints) and the
// highest published version satisfying it wins.
func (catalog Catalog) Resolve(name, version, architecture string) (Resolved, error) {
	for _, layer := range catalog.Layers {
		if layer.Name != name {
			continue
		}

		resolvedVersion, err := layer.resolveVersion(version)
		if err != nil {
			return Resolved{}, err
		}
		for _, tag := range layer.Tags {
			if tag.Version != resolvedVersion {
				continue
			}

			artifact, found := tag.Sysext[architecture]
			if !found {
				return Resolved{}, fmt.Errorf("architecture %q is not available for layer %q version %q", architecture, name, resolvedVersion)
			}
			if !immutableOCIReference.MatchString(artifact.OCI) {
				return Resolved{}, fmt.Errorf("OCI reference for layer %q version %q architecture %q must end with @sha256: and 64 hexadecimal characters", name, resolvedVersion, architecture)
			}

			return Resolved{
				Repository:   catalog.Repository,
				Name:         name,
				Version:      resolvedVersion,
				Architecture: architecture,
				OCI:          artifact.OCI,
			}, nil
		}
		return Resolved{}, fmt.Errorf("version %q is not available for layer %q", resolvedVersion, name)
	}

	return Resolved{}, fmt.Errorf("layer %q is not available", name)
}

// resolveVersion turns a requested version into one published tag.
func (layer Layer) resolveVersion(requested string) (string, error) {
	if requested == "" {
		return layer.Latest, nil
	}
	for _, tag := range layer.Tags {
		if tag.Version == requested {
			return requested, nil
		}
	}

	constraint, err := semver.NewConstraint(requested)
	if err != nil {
		// Not a published tag and not a constraint either: report the request
		// as the missing version rather than as a syntax error, which is what
		// it is from the caller's point of view.
		return requested, nil
	}

	var best *semver.Version
	var bestTag string
	for _, tag := range layer.Tags {
		candidate, err := semver.NewVersion(tag.Version)
		if err != nil {
			continue
		}
		if !constraint.Check(candidate) {
			continue
		}
		if best == nil || candidate.GreaterThan(best) {
			best, bestTag = candidate, tag.Version
		}
	}
	if best == nil {
		return "", fmt.Errorf("no version of layer %q satisfies %q", layer.Name, requested)
	}
	return bestTag, nil
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
