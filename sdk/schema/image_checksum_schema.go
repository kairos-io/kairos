package schema

import (
	jsonschemago "github.com/swaggest/jsonschema-go"
)

// ImageChecksumSchema represents the image-checksum block in the Kairos
// configuration. It mirrors sdk/imagesum.Config. Each key can also be set on the
// kernel cmdline in dot notation (image-checksum.create=true), which wins over
// the cloud config.
type ImageChecksumSchema struct {
	_             struct{} `title:"Kairos Schema: Image checksum block" description:"Checksums of the OS images (active, passive, recovery), written by kairos-agent and checked by immucore at boot to catch disk corruption."`
	Create        bool     `json:"create,omitempty" default:"false" description:"Write a sha256 file next to each OS image when installing, upgrading or resetting. Stale checksum files are removed even when this is off."`
	Verify        string   `json:"verify,omitempty" default:"none" enum:"[\"none\",\"warn\",\"fail\"]" description:"What immucore does with the checksum before mounting an image: none does not check, warn logs problems and boots, fail stops the boot with a failure screen. The boot time value can be overridden with image-checksum.verify= on the kernel cmdline."`
	VerifyTimeout int      `json:"verify-timeout,omitempty" minimum:"0" default:"60" description:"Seconds immucore may spend hashing an image at boot. When it runs out the check is skipped with a warning and the boot goes on, in warn and in fail mode, so a slow disk cannot stall the boot into the initramfs timeout. 0 means no limit. Can be overridden with image-checksum.verify-timeout= on the kernel cmdline."`
	Require       bool     `json:"require,omitempty" default:"false" description:"Treat an image with no usable checksum (missing or stale) as a problem. Off by default so systems installed before checksums existed keep booting."`
}

var _ jsonschemago.Preparer = ImageChecksumSchema{}

// PrepareJSONSchema closes the block to keys it does not declare. A misspelled
// verify or require silently leaves checking off, which is the one thing this
// block must not do quietly.
func (ImageChecksumSchema) PrepareJSONSchema(schema *jsonschemago.Schema) error {
	schema.WithAdditionalProperties(*(&jsonschemago.SchemaOrBool{}).WithTypeBoolean(false))

	return nil
}
