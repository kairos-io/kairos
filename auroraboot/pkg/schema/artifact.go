package schema

import (
	"fmt"
	"strings"

	"golang.org/x/mod/semver"
)

type ReleaseArtifact struct {
	ArtifactVersion string `yaml:"artifact_version"`
	Model           string `yaml:"model"`
	Flavor          string `yaml:"flavor"`
	FlavorRelease   string `yaml:"flavor_release"`
	Platform        string `yaml:"platform"`
	ReleaseVersion  string `yaml:"release_version"`
	Repository      string `yaml:"repository"`
	Variant         string `yaml:"variant"`

	ContainerImage string `yaml:"container_image"`
}

func (a ReleaseArtifact) FileName() string {
	if a.ContainerImage != "" {
		return ""
	}

	if a.Model == "" {
		a.Model = "generic"
	}
	if a.Platform == "" {
		a.Platform = "amd64"
	}
	if a.Variant == "" {
		if strings.Contains(a.ArtifactVersion, "k3s") {
			a.Variant = "standard"
		} else {
			a.Variant = "core"
		}

	}

	if semver.Compare(a.ReleaseVersion, "v2.4.0") < 0 {
		variant := a.Variant
		if variant == "standard" {
			variant = "kairos"
		}
		return fmt.Sprintf("%s-%s-%s", variant, a.Flavor, a.ArtifactVersion)
	}

	if semver.Compare(a.ReleaseVersion, "v2.4.2") < 0 {
		return fmt.Sprintf("kairos-%s-%s-%s-generic-%s", a.Variant, a.Flavor, a.Platform, a.ArtifactVersion)
	}

	return fmt.Sprintf("kairos-%s-%s-%s-%s-generic-%s", a.Flavor, a.FlavorRelease, a.Variant, a.Platform, a.ArtifactVersion)
}

func (a ReleaseArtifact) urlGen(ext string) string {
	return fmt.Sprintf("https://github.com/%s/releases/download/%s/%s%s", a.Repository, a.ReleaseVersion, a.FileName(), ext)
}

func (a ReleaseArtifact) NetbootArtifacts() []string {
	return []string{a.InitrdURL()}
}

func (a ReleaseArtifact) ISOUrl() string {
	return a.urlGen(".iso")
}

func (a ReleaseArtifact) InitrdURL() string {
	return a.urlGen("-initrd")
}

func (a ReleaseArtifact) KernelURL() string {
	return a.urlGen("-kernel")
}

func (a ReleaseArtifact) SquashFSURL() string {
	return a.urlGen(".squashfs")
}
