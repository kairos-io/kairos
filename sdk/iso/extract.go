package iso

import (
	"fmt"

	"github.com/diskfs/go-diskfs"
	sdkLogger "github.com/kairos-io/kairos-sdk/types/logger"

	"io"
	"os"
	"path/filepath"
	"strings"
)

// ExtractFileFromIso will extract a given file from a given iso to a given destination
func ExtractFileFromIso(file, iso, destination string, logger *sdkLogger.KairosLogger) (err error) {
	if logger == nil {
		l := sdkLogger.NewNullLogger()
		logger = &l
	}
	// set a sublogger with the args
	log := logger.Logger.With().Str("file", file).Str("iso", iso).Str("destination", destination).Logger()
	_, err = os.Stat(iso)
	if err != nil {
		log.Error().Err(err).Msg("checking iso file")
		return fmt.Errorf("error checking on %s: %s", iso, err.Error())
	}
	if !isFullPath(file) {
		log.Error().Err(err).Msg("file to extract is not a full path")
		return fmt.Errorf("%s is not a full path", file)
	}

	log.Trace().Msg("Opening iso file")
	log.Debug().Msg("Extracting file from iso")
	open, err := diskfs.Open(iso, diskfs.WithOpenMode(diskfs.ReadOnly))
	if err != nil {
		log.Error().Err(err).Msg("opening iso file")
		return err
	}
	log.Trace().Msg("Getting filesystem")
	fs, err := open.GetFilesystem(0)
	if err != nil {
		log.Error().Err(err).Msg("getting filesystem")
		return err
	}
	log.Trace().Msg("Opening file inside iso")
	isoFile, err := fs.OpenFile(file, os.O_RDONLY)
	if err != nil {
		log.Error().Err(err).Msg("opening file inside iso")
		return err
	}
	defer isoFile.Close()
	log.Trace().Msg("Opening destination file")
	destFile, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		log.Error().Err(err).Msg("creating destination file")
		return err
	}
	defer destFile.Close()

	log.Trace().Msg("Copying file to destination")
	// Copy isoFile to destFile
	_, err = io.Copy(destFile, isoFile)
	if err != nil {
		log.Error().Err(err).Msg("copying file to destination")
		return err
	}
	log.Debug().Msg("File extracted from iso")
	return err
}

// isFullPath checks a given path to see if its absolute
// removes any relative thingies like .. or .../
// checks that it starts with /
// checks that we dont refer to the full root dir
func isFullPath(path string) bool {
	if path == "" {
		return false
	}
	if !strings.HasPrefix(path, "/") {
		return false
	}
	cleaned := filepath.Clean(path)
	if !filepath.IsAbs(cleaned) {
		return false
	}
	if cleaned == "/" {
		return false
	}
	return len(strings.Split(cleaned, "/")) > 1
}
