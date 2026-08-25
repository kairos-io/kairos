// Package extensions parses and resolves published system extension catalogs.
package extensions

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"
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
func (catalog Catalog) Resolve(name, version, architecture string) (Resolved, error) {
	for _, layer := range catalog.Layers {
		if layer.Name != name {
			continue
		}

		resolvedVersion := version
		if resolvedVersion == "" {
			resolvedVersion = layer.Latest
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
