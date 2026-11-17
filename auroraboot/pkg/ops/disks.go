package ops

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/kairos-io/AuroraBoot/internal"
	"github.com/kairos-io/kairos/v4/sdk/utils"
)

// rawDiskCloudConfigFile is the cloud config written to the raw disk output dir by a previous step.
const rawDiskCloudConfigFile = "config.yaml"

func GenEFIRawDisk(params RawImageParams) func(ctx context.Context) error {
	return genRawDisk(params, NewEFIRawImage)
}

func GenBiosRawDisk(params RawImageParams) func(ctx context.Context) error {
	return genRawDisk(params, NewBiosRawImage)
}

func genRawDisk(params RawImageParams, newImage func(RawImageParams) *RawImage) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		internal.Log.Logger.Info().Msgf("Generating raw disk '%s' from '%s' with final size %dMb", params.Output, params.Source, params.FinalSize)
		// TODO: We need to talk about how the config.yaml is magically here no? is done in a previous step but maybe we should have constant that we can check?
		// Maybe on its own function that returns the tmpdir + config.yaml or something? we need a safe way of accessing it form any step in the DAG.
		params.CloudConfig = filepath.Join(params.Output, rawDiskCloudConfigFile)
		raw := newImage(params)

		images, cleanup, err := materializeDiskExtensions(ctx, params.Extensions.withArchFrom(params.Source))
		if err != nil {
			internal.Log.Logger.Error().Err(err).Msg("Resolving the extensions to bundle into the raw disk failed")
			return err
		}
		defer cleanup()
		raw.ExtensionFiles = images

		err = raw.Build()
		if err != nil {
			internal.Log.Logger.Error().Msgf("Generating raw disk '%s' from '%s' failed with error '%s'", params.Output, params.Source, err.Error())
		}
		return err
	}
}

func ExtractSquashFS(srcFunc, dstFunc valueGetOnCall) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		src := srcFunc()
		dst := dstFunc()
		tmp, err := os.MkdirTemp("", "gendisk")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmp)

		internal.Log.Logger.Info().Msgf("unpacking to '%s' the squashfs file: '%s'", dst, src)
		out, err := utils.SH(fmt.Sprintf("unsquashfs -f -d %s %s", dst, src))
		internal.Log.Logger.Printf("Output '%s'", out)
		if err != nil {
			internal.Log.Logger.Error().Msgf("unpacking to '%s' from '%s' failed with error '%s'", dst, src, err.Error())
		}
		return err
	}
}

func ConvertRawDiskToVHD(src string) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		internal.Log.Logger.Info().Str("dir", src).Msg("Converting raw disk to VHD")
		output, err := convertRawOnCopy(src, Raw2Azure)
		if err != nil {
			internal.Log.Logger.Error().Err(err).Str("dir", src).Msg("Converting raw disk to VHD failed")
			return err
		}
		internal.Log.Logger.Info().Msgf("Generated VHD disk '%s'", output)
		return nil
	}
}

func ConvertRawDiskToGCE(src string) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		internal.Log.Logger.Info().Str("dir", src).Msg("Converting raw disk to GCE")
		output, err := convertRawOnCopy(src, Raw2Gce)
		if err != nil {
			internal.Log.Logger.Error().Err(err).Str("dir", src).Msg("Converting raw disk to GCE failed")
			return err
		}
		internal.Log.Logger.Info().Msgf("Generated GCE disk '%s'", output)
		return nil
	}
}

// convertRawOnCopy runs a Raw2X converter against a PRIVATE COPY of the single
// kairos-*.raw in dir, then moves the produced artifact back into dir under its
// conventional <raw>.<ext> name. Working on a copy is what makes the cloud-image
// conversions safe to run in parallel: Raw2Gce truncates its source in place and
// Raw2Azure renames it away, so pointing them all at the one shared raw races
// (and corrupts the raw itself, which may be a requested output). Each converter
// instead mutates its own throwaway copy and leaves the original raw untouched.
//
// The work directory is a subdirectory of dir so both the copy and the final
// os.Rename stay on the same filesystem. It is nested one level down, so a
// concurrent converter's top-level `kairos-*.raw` glob never matches the copy.
func convertRawOnCopy(dir string, convert func(source string) (string, error)) (string, error) {
	glob, err := filepath.Glob(filepath.Join(dir, "kairos-*.raw"))
	if err != nil {
		return "", err
	}
	if len(glob) != 1 {
		return "", fmt.Errorf("expected to find one and only one raw disk file in '%s' but found %d", dir, len(glob))
	}
	raw := glob[0]

	work, err := os.MkdirTemp(dir, ".convert-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(work)

	workRaw := filepath.Join(work, filepath.Base(raw))
	if err := copyFile(raw, workRaw); err != nil {
		return "", fmt.Errorf("copying raw disk for conversion: %w", err)
	}

	output, err := convert(workRaw)
	if err != nil {
		return "", err
	}

	dst := filepath.Join(dir, filepath.Base(output))
	if err := os.Rename(output, dst); err != nil {
		return "", fmt.Errorf("moving converted disk into place: %w", err)
	}
	return dst, nil
}

// copyFile copies the contents of src to a new file at dst.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
