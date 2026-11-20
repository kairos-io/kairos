package deployer

import (
	"fmt"
	"io"
	"os"
)

// Rework this

// There should be an inputs part, that is the input for the artifact, be it download, generate whatever and that gives us a rootfs with the files on it
// then a process part, which process the rootfs and add stuff, like clouud config
// then an output part which transforms that rootfs into an artifact, be it an ISO, a disk image, netboot files, etc.
// then a run part, which is the netboot server, the http server, etc.

// RegisterAll registers the op dag based on the configuration and the artifact wanted.
// This registers all steps for the top level Auroraboot command.
func RegisterAll(d *Deployer) error {
	if err := d.Config.Validate(); err != nil {
		return err
	}
	for _, step := range []func() error{
		d.PrepDirs,
		d.StepCopyCloudConfig,
		d.StepDumpSource,
		d.StepGenISO,
		d.StepDownloadISO,
		d.StepExtractNetboot,
		// Ops to generate RAW disk images
		d.StepGenRawDisk,
		d.StepGenMBRRawDisk,
		d.StepConvertGCE,
		d.StepConvertVHD,
		d.StepConvertMAAS,
		// Inject the data into the ISO
		d.StepInjectCC,
		// Start servers
		d.StepStartHTTPServer,
		d.StepStartNetboot,
	} {
		if err := step(); err != nil {
			return err
		}
	}
	return nil
}

func copy(src, dst string) (int64, error) {
	sourceFileStat, err := os.Stat(src)
	if err != nil {
		return 0, err
	}

	if !sourceFileStat.Mode().IsRegular() {
		return 0, fmt.Errorf("%s is not a regular file", src)
	}

	source, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer source.Close()

	destination, err := os.Create(dst)
	if err != nil {
		return 0, err
	}
	defer destination.Close()
	nBytes, err := io.Copy(destination, source)
	return nBytes, err
}
