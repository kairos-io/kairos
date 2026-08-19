package image

import (
	imagetypes "github.com/kairos-io/kairos-sdk/types/images"
	"github.com/kairos-io/kairos-sdk/utils"
)

// OCIImageExtractor is the default implementation of imagetypes.ImageExtractor:
// it pulls an OCI image with GetImage and unpacks it with ExtractOCIImage.
//
// Set Insecure to allow pulling from registries served over plain HTTP or
// presenting an untrusted/self-signed TLS certificate (see WithInsecureRegistry).
type OCIImageExtractor struct {
	Insecure bool
}

var _ imagetypes.ImageExtractor = OCIImageExtractor{}

// pullOptions translates the extractor's configuration into GetImage options.
func (e OCIImageExtractor) pullOptions() []GetOption {
	if e.Insecure {
		return []GetOption{WithInsecureRegistry()}
	}
	return nil
}

// resolvePlatform defaults to the current host platform only when no platform
// was requested. An explicit platformRef is passed through untouched so GetImage
// validates it and surfaces an error, rather than silently falling back to the
// host platform and pulling the wrong image.
func resolvePlatform(platformRef string) string {
	if platformRef == "" {
		return utils.GetCurrentPlatform()
	}
	return platformRef
}

func (e OCIImageExtractor) ExtractImage(imageRef, destination, platformRef string, excludes ...string) error {
	img, err := GetImage(imageRef, resolvePlatform(platformRef), nil, nil, e.pullOptions()...)
	if err != nil {
		return err
	}
	return ExtractOCIImage(img, destination, excludes...)
}

func (e OCIImageExtractor) GetOCIImageSize(imageRef, platformRef string) (int64, error) {
	return GetOCIImageSize(imageRef, resolvePlatform(platformRef), nil, nil, e.pullOptions()...)
}
