package versioneer

import (
	"fmt"

	"github.com/google/go-containerregistry/pkg/crane"
)

type RegistryInspector interface {
	TagList(registryAndOrg string, artifact *Artifact) (TagList, error)
}

type DefaultRegistryInspector struct{}

func (i *DefaultRegistryInspector) TagList(registryAndOrg string, artifact *Artifact) (TagList, error) {
	var err error
	tl := TagList{
		Artifact:       artifact,
		RegistryAndOrg: registryAndOrg,
	}

	tl.Tags, err = crane.ListTags(fmt.Sprintf("%s/%s", registryAndOrg, artifact.Flavor))
	if err != nil {
		return tl, err
	}

	return tl, nil
}
