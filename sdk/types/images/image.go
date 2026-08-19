package images

import (
	"fmt"
	"net/url"
	"path/filepath"

	"github.com/distribution/reference"
	"gopkg.in/yaml.v3"
)

const (
	docker  = "docker"
	oci     = "oci"
	file    = "file"
	dir     = "dir"
	ocifile = "ocifile"
)

// Image struct represents a file system image with its commonly configurable values, size in MiB.
type Image struct {
	File       string       `yaml:"-" json:"-"` // File is not serialized
	Label      string       `yaml:"label,omitempty" mapstructure:"label" json:"label,omitempty"`
	Size       uint         `yaml:"size,omitempty" mapstructure:"size" json:"size,omitempty"`
	FS         string       `yaml:"fs,omitempty" mapstructure:"fs" json:"fs,omitempty"`
	URI        string       `yaml:"uri,omitempty" mapstructure:"uri" json:"uri,omitempty"` // deprecated, use Source instead
	Source     *ImageSource `yaml:"source,omitempty" mapstructure:"source" json:"source,omitempty"`
	MountPoint string       `yaml:"-" json:"-"` // MountPoint is not serialized
	LoopDevice string       `yaml:"-" json:"-"` // LoopDevice is not serialized
}
type ImageSource struct {
	source  string `yaml:"source" json:"source"` //nolint:govet
	srcType string `yaml:"type" json:"type"`     //nolint:govet
}

// Implement the Imagesource methods here so everything can consume it.

func (i ImageSource) Value() string {
	return i.source
}

func (i ImageSource) IsDocker() bool {
	return i.srcType == oci
}

func (i ImageSource) IsDir() bool {
	return i.srcType == dir
}

func (i ImageSource) IsFile() bool {
	return i.srcType == file
}

func (i ImageSource) IsOCIFile() bool {
	return i.srcType == ocifile
}

func (i ImageSource) IsEmpty() bool {
	if i.srcType == "" {
		return true
	}
	if i.source == "" {
		return true
	}
	return false
}

func (i ImageSource) String() string {
	if i.IsEmpty() {
		return ""
	}
	return fmt.Sprintf("%s://%s", i.srcType, i.source)
}

func (i ImageSource) MarshalYAML() (interface{}, error) {
	return i.String(), nil
}

func (i *ImageSource) UnmarshalYAML(value *yaml.Node) error {
	return i.updateFromURI(value.Value)
}

func (i *ImageSource) CustomUnmarshal(data interface{}) (bool, error) {
	src, ok := data.(string)
	if !ok {
		return false, fmt.Errorf("can't unmarshal %+v to an ImageSource type", data)
	}
	err := i.updateFromURI(src)
	return false, err
}

func (i *ImageSource) updateFromURI(uri string) error {
	u, err := url.Parse(uri)
	if err != nil {
		return err
	}
	scheme := u.Scheme
	value := u.Opaque
	if value == "" {
		value = filepath.Join(u.Host, u.Path)
	}
	switch scheme {
	case oci, docker:
		return i.parseImageReference(value)
	case dir:
		i.srcType = dir
		i.source = value
	case file:
		i.srcType = file
		i.source = value
	case ocifile:
		i.srcType = ocifile
		i.source = value
	default:
		return i.parseImageReference(uri)
	}
	return nil
}

func (i *ImageSource) parseImageReference(ref string) error {
	n, err := reference.ParseNormalizedNamed(ref)
	if err != nil {
		return fmt.Errorf("invalid image reference %s", ref)
	} else if reference.IsNameOnly(n) {
		ref += ":latest"
	}
	i.srcType = oci
	i.source = ref
	return nil
}

func NewSrcFromURI(uri string) (*ImageSource, error) {
	src := ImageSource{}
	err := src.updateFromURI(uri)
	return &src, err
}

func NewEmptySrc() *ImageSource {
	return &ImageSource{}
}

func NewDockerSrc(src string) *ImageSource {
	return &ImageSource{source: src, srcType: oci}
}

func NewFileSrc(src string) *ImageSource {
	return &ImageSource{source: src, srcType: file}
}

func NewDirSrc(src string) *ImageSource {
	return &ImageSource{source: src, srcType: dir}
}

func NewOCIFileSrc(src string) *ImageSource {
	return &ImageSource{source: src, srcType: ocifile}
}

type ImageExtractor interface {
	ExtractImage(imageRef, destination, platformRef string, excludes ...string) error
	GetOCIImageSize(imageRef, platformRef string) (int64, error)
}
