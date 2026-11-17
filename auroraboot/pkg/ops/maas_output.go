package ops

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"

	"github.com/kairos-io/AuroraBoot/internal"
)

// Raw2MAAS gzips a raw disk image into the "ddgz" format MAAS expects for a
// custom uploaded image (boot-resources create ... filetype=ddgz). The raw is
// left in place; a sibling <raw>.gz is produced.
func Raw2MAAS(source string) (string, error) {
	internal.Log.Logger.Info().Str("source", source).Msg("Compressing raw image into MAAS ddgz format")
	name := fmt.Sprintf("%s.gz", source)

	in, err := os.Open(source)
	if err != nil {
		internal.Log.Logger.Error().Err(err).Str("file", source).Msg("Error opening raw image")
		return name, err
	}
	defer in.Close()

	out, err := os.Create(name)
	if err != nil {
		internal.Log.Logger.Error().Err(err).Str("file", name).Msg("Error creating gzip output")
		return name, err
	}
	defer out.Close()

	gw, err := gzip.NewWriterLevel(out, gzip.BestSpeed)
	if err != nil {
		internal.Log.Logger.Error().Err(err).Str("file", name).Msg("Error creating gzip writer")
		return name, err
	}
	if _, err := io.Copy(gw, in); err != nil {
		internal.Log.Logger.Error().Err(err).Str("file", source).Msg("Error compressing raw image")
		_ = gw.Close()
		return name, err
	}
	if err := gw.Close(); err != nil {
		internal.Log.Logger.Error().Err(err).Str("file", name).Msg("Error finalizing gzip stream")
		return name, err
	}
	if err := out.Sync(); err != nil {
		return name, err
	}
	return name, nil
}

// ConvertRawDiskToMAAS finds the single raw disk in src and compresses it into
// the MAAS ddgz format. Mirrors ConvertRawDiskToGCE / ConvertRawDiskToVHD: it
// works on a private copy of the raw so a concurrent GCE/VHD conversion (which
// truncates/renames the shared raw) cannot make this read a torn image, and the
// original raw is left untouched.
func ConvertRawDiskToMAAS(src string) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		internal.Log.Logger.Info().Str("dir", src).Msg("Compressing raw disk for MAAS")
		output, err := convertRawOnCopy(src, Raw2MAAS)
		if err != nil {
			internal.Log.Logger.Error().Err(err).Str("dir", src).Msg("Compressing raw disk for MAAS failed")
			return err
		}
		internal.Log.Logger.Info().Msgf("Generated MAAS ddgz image '%s'", output)
		return nil
	}
}
