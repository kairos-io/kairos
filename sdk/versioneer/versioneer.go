package versioneer

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/kairos-io/kairos-sdk/utils"
)

const (
	// KAIROS_VERSION was already used in kairos-release and we avoided breaking it
	// for consumers by using a new variable KAIROS_RELEASE instead. But it's the
	// "Artifact.Version".
	EnvVarVersion               = "RELEASE"
	EnvVarFlavor                = "FLAVOR"
	EnvVarFlavorRelease         = "FLAVOR_RELEASE"
	EnvVarVariant               = "VARIANT"
	EnvVarModel                 = "MODEL"
	EnvVarArch                  = "TARGETARCH"
	EnvVarSoftwareVersion       = "SOFTWARE_VERSION"
	EnvVarSoftwareVersionPrefix = "SOFTWARE_VERSION_PREFIX"
	EnvVarRegistryAndOrg        = "REGISTRY_AND_ORG"
	EnvVarID                    = "ID"
	EnvVarGithubRepo            = "GITHUB_REPO"
	EnvVarBugReportURL          = "BUG_REPORT_URL"
	EnvVarHomeURL               = "HOME_URL"
	EnvVarFamily                = "FAMILY"
)

type Artifact struct {
	Flavor                string
	Family                string
	FlavorRelease         string
	Variant               string
	Model                 string
	Arch                  string
	Version               string // The Kairos version. E.g. "v2.4.2"
	SoftwareVersion       string // The k3s version. E.g. "v1.26.9+k3s1"
	SoftwareVersionPrefix string // E.g. k3s
	RegistryInspector     RegistryInspector
}

func NewArtifactFromJSON(jsonStr string) (*Artifact, error) {
	result := &Artifact{}
	err := json.Unmarshal([]byte(jsonStr), result)

	return result, err
}

// NewArtifactFromOSRelease generates an artifact by inspecting the variables
// in the /etc/kairos-release file of a Kairos image. The variable should be
// prefixed with "KAIROS_". E.g. KAIROS_VARIANT would be used to set the Variant
// field. The function optionally takes an argument to specify a different file
// path (for testing reasons).
func NewArtifactFromOSRelease(file ...string) (*Artifact, error) {
	var err error
	result := Artifact{}

	if result.Flavor, err = utils.OSRelease(EnvVarFlavor, file...); err != nil {
		return nil, err
	}
	if result.Family, err = utils.OSRelease(EnvVarFamily, file...); err != nil {
		return nil, err
	}
	if result.FlavorRelease, err = utils.OSRelease(EnvVarFlavorRelease, file...); err != nil {
		return nil, err
	}
	if result.Variant, err = utils.OSRelease(EnvVarVariant, file...); err != nil {
		return nil, err
	}
	if result.Model, err = utils.OSRelease(EnvVarModel, file...); err != nil {
		return nil, err
	}
	if result.Arch, err = utils.OSRelease(EnvVarArch, file...); err != nil {
		return nil, err
	}
	if result.Version, err = utils.OSRelease(EnvVarVersion, file...); err != nil {
		return nil, err
	}

	// Optional, could be missing
	result.SoftwareVersion, err = utils.OSRelease(EnvVarSoftwareVersion, file...)
	if err != nil && !errors.As(err, &utils.KeyNotFoundErr{}) {
		return nil, err
	}

	// Optional, could be missing
	result.SoftwareVersionPrefix, err = utils.OSRelease(EnvVarSoftwareVersionPrefix, file...)
	if err != nil && !errors.As(err, &utils.KeyNotFoundErr{}) {
		return nil, err
	}

	return &result, nil
}

func (a *Artifact) Validate() error {
	if a.Variant == "" {
		return errors.New("Variant is empty")
	}

	return a.ValidateBase()
}

func (a *Artifact) ValidateBase() error {
	if a.FlavorRelease == "" {
		return errors.New("FlavorRelease is empty")
	}
	if a.Model == "" {
		return errors.New("Model is empty")
	}
	if a.Arch == "" {
		return errors.New("Arch is empty")
	}

	if a.SoftwareVersion != "" && a.SoftwareVersionPrefix == "" {
		return errors.New("SoftwareVersionPrefix should be defined when SoftwareVersion is not empty")
	}
	return nil
}

func (a *Artifact) BootableName() (string, error) {
	commonName, err := a.commonVersionedName()
	if err != nil {
		return "", err
	}

	if a.Flavor == "" {
		return "", errors.New("Flavor is empty")
	}

	return fmt.Sprintf("kairos-%s-%s", a.Flavor, commonName), nil
}

func (a *Artifact) Repository(registryAndOrg string) string {
	return fmt.Sprintf("%s/%s", registryAndOrg, a.Flavor)
}

func (a *Artifact) ContainerName(registryAndOrg string) (string, error) {
	if a.Flavor == "" {
		return "", errors.New("Flavor is empty")
	}

	tag, err := a.Tag()
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%s:%s", a.Repository(registryAndOrg), tag), nil
}

func (a *Artifact) BaseContainerName(registryAndOrg, id string) (string, error) {
	if a.Flavor == "" {
		return "", errors.New("Flavor is empty")
	}

	if id == "" {
		return "", errors.New("no id passed")
	}

	tag, err := a.BaseTag()
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%s:%s-%s", a.Repository(registryAndOrg), tag, id), nil
}

func (a *Artifact) BaseTag() (string, error) {
	if err := a.ValidateBase(); err != nil {
		return "", err
	}

	result := fmt.Sprintf("%s-%s-%s",
		a.FlavorRelease, a.Arch, a.Model)

	return result, nil
}

func (a *Artifact) Tag() (string, error) {
	commonName, err := a.commonVersionedName()
	if err != nil {
		return commonName, err
	}

	return strings.ReplaceAll(commonName, "+", "-"), nil
}

// VersionForTag replaces and "+" symbols with "-" because in container image
// tags, "+" is not valid
func (a *Artifact) VersionForTag() string {
	return strings.ReplaceAll(a.Version, "+", "-")
}

// SoftwareVersionForTag replaces and "+" symbols with "-" because in container image
// tags, "+" is not valid
func (a *Artifact) SoftwareVersionForTag() string {
	return strings.ReplaceAll(a.SoftwareVersion, "+", "-")
}

// OSReleaseVariables returns a set of variables to be appended in /etc/kairos-release
func (a *Artifact) OSReleaseVariables(registryAndOrg, githubRepo, bugURL, homeURL string) (string, error) {
	if registryAndOrg == "" {
		return "", errors.New("registry-and-org must be set")
	}
	commonName, err := a.commonVersionedName()
	if err != nil {
		return commonName, err
	}
	kairosName := fmt.Sprintf("kairos-%s-%s-%s", a.Variant, a.Flavor, a.FlavorRelease)
	kairosVersion := a.Version
	if a.SoftwareVersion != "" {
		kairosVersion += "-" + strings.ReplaceAll(a.SoftwareVersion, "+", "-")
	}

	containerName, err := a.ContainerName(registryAndOrg)
	if err != nil {
		return "", err
	}

	tag, err := a.Tag()
	if err != nil {
		return "", err
	}

	bootableName, err := a.BootableName()
	if err != nil {
		return "", err
	}

	vars := map[string]string{
		// Legacy variables (not used by versioneer)
		"KAIROS_NAME":        kairosName,
		"KAIROS_VERSION":     kairosVersion,
		"KAIROS_ID":          "kairos",
		"KAIROS_ID_LIKE":     kairosName,
		"KAIROS_VERSION_ID":  kairosVersion,
		"KAIROS_PRETTY_NAME": fmt.Sprintf("%s %s", kairosName, kairosVersion),
		"KAIROS_IMAGE_REPO":  containerName,
		"KAIROS_IMAGE_LABEL": tag,
		"KAIROS_ARTIFACT":    bootableName,
		// Actively used variables
		"KAIROS_FLAVOR":           a.Flavor,
		"KAIROS_FLAVOR_RELEASE":   a.FlavorRelease,
		"KAIROS_FAMILY":           a.Family,
		"KAIROS_VARIANT":          a.Variant,
		"KAIROS_MODEL":            a.Model,
		"KAIROS_TARGETARCH":       a.Arch,
		"KAIROS_RELEASE":          a.Version,
		"KAIROS_REGISTRY_AND_ORG": registryAndOrg,
	}
	if bugURL != "" {
		vars["KAIROS_BUG_REPORT_URL"] = bugURL
	}
	if homeURL != "" {
		vars["KAIROS_HOME_URL"] = homeURL
	}
	if githubRepo != "" {
		vars["KAIROS_GITHUB_REPO"] = githubRepo
	}
	if a.SoftwareVersion != "" {
		vars["KAIROS_SOFTWARE_VERSION"] = a.SoftwareVersion
	}
	if a.SoftwareVersionPrefix != "" {
		vars["KAIROS_SOFTWARE_VERSION_PREFIX"] = a.SoftwareVersionPrefix
	}

	result := ""
	for k, v := range vars {
		result += fmt.Sprintf("%s=\"%s\"\n", k, v)
	}

	return result, nil
}

func (a *Artifact) TagList(registryAndOrg string) (TagList, error) {
	if a.RegistryInspector == nil {
		a.RegistryInspector = &DefaultRegistryInspector{}
	}

	return a.RegistryInspector.TagList(registryAndOrg, a)
}

func (a *Artifact) commonName() (string, error) {
	if err := a.Validate(); err != nil {
		return "", err
	}

	result := fmt.Sprintf("%s-%s-%s-%s",
		a.FlavorRelease, a.Variant, a.Arch, a.Model)

	return result, nil
}

func (a *Artifact) commonVersionedName() (string, error) {
	if a.Version == "" {
		return "", errors.New("Version is empty")
	}

	result, err := a.commonName()
	if err != nil {
		return result, err
	}

	result = fmt.Sprintf("%s-%s", result, a.Version)

	if a.SoftwareVersion != "" {
		result = fmt.Sprintf("%s-%s%s", result, a.SoftwareVersionPrefix, a.SoftwareVersion)
	}

	return result, nil
}
