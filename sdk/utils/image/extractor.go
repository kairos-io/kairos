package image

import (
	"time"

	"github.com/google/go-containerregistry/pkg/logs"

	imagetypes "github.com/kairos-io/kairos/v4/sdk/types/images"
	"github.com/kairos-io/kairos/v4/sdk/utils"
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

// pullAttempts is the total number of tries ExtractImage gives a pull, and
// pullRetryDelay the wait before the second one; each further wait is
// pullRetryFactor times the previous.
const (
	pullAttempts    = 3
	pullRetryDelay  = 1 * time.Second
	pullRetryFactor = 3
)

// ExtractImage pulls imageRef and unpacks it into destination, retrying the
// whole operation when it fails on a transient network error.
//
// The retry is here, around both steps, rather than in the registry client,
// because that client only retries whole requests. Once a blob response is
// streaming, a reset connection or a broken HTTP/2 stream surfaces as a read
// error part-way through a layer, and nothing below this point resumes or
// restarts it. On a system image that is minutes of download thrown away by
// one hiccup: it is how kairos-io/kairos#4491 aborted an upgrade after ten
// minutes on a PROTOCOL_ERROR.
//
// Reapplying the layers over a part-extracted destination is safe. The same
// layers are applied in the same order, so every write and every whiteout the
// abandoned attempt performed is performed again, and the tree the last
// attempt leaves behind is the tree a clean run would have produced.
func (e OCIImageExtractor) ExtractImage(imageRef, destination, platformRef string, excludes ...string) error {
	var err error

	delay := pullRetryDelay
	for attempt := 1; ; attempt++ {
		err = e.extract(imageRef, destination, platformRef, excludes...)
		if err == nil || attempt == pullAttempts || !isTransientNetworkError(err) {
			return err
		}

		logs.Warn.Printf("pulling %s failed (attempt %d of %d), retrying in %s: %v",
			imageRef, attempt, pullAttempts, delay, err)
		time.Sleep(delay)
		delay *= pullRetryFactor
	}
}

func (e OCIImageExtractor) extract(imageRef, destination, platformRef string, excludes ...string) error {
	img, err := GetImage(imageRef, resolvePlatform(platformRef), nil, nil, e.pullOptions()...)
	if err != nil {
		return err
	}
	return ExtractOCIImage(img, destination, excludes...)
}

func (e OCIImageExtractor) GetOCIImageSize(imageRef, platformRef string) (int64, error) {
	return GetOCIImageSize(imageRef, resolvePlatform(platformRef), nil, nil, e.pullOptions()...)
}
