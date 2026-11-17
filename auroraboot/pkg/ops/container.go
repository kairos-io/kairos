package ops

import (
	"context"
	"fmt"

	"github.com/kairos-io/AuroraBoot/internal"
	"github.com/kairos-io/kairos/v4/agent/pkg/elemental"
	sdkImage "github.com/kairos-io/kairos/v4/sdk/types/images"
	imageutils "github.com/kairos-io/kairos/v4/sdk/utils/image"
)

// DumpSource pulls a container image either remotely or locally from a docker daemon
// or simply copies the directory to the destination.
// When allowInsecureRegistries is true, pulls are allowed from plain-HTTP
// registries and from registries presenting untrusted/self-signed TLS
// certificates.
// Supports these prefixes:
// https://github.com/kairos-io/kairos/blob/b5e8809b4aeedb9cedf73b02b85cb8dc15c47e26/sdk/types/images/image.go#L13-L17
func DumpSource(image string, dstFunc valueGetOnCall, arch string, allowInsecureRegistries bool) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		dst := dstFunc()
		if image == "" {
			return fmt.Errorf("image source is empty, cannot dump to %s", dst)
		}
		internal.Log.Logger.Debug().Str("arch", arch).Bool("allow-insecure-registries", allowInsecureRegistries).Msg("DumpSource: arch parameter")

		opts := []GenericOptions{
			WithImageExtractor(imageutils.OCIImageExtractor{Insecure: allowInsecureRegistries}),
			WithLogger(internal.Log),
		}
		if arch != "" {
			opts = append(opts, WithArch(arch))
		}
		cfg, err := NewConfig(opts...)
		if err != nil {
			return fmt.Errorf("building the config to dump %s: %w", image, err)
		}
		internal.Log.Logger.Debug().Str("arch", cfg.Arch).Msg("DumpSource: config arch after NewConfig")
		if cfg.Platform != nil {
			internal.Log.Logger.Debug().Str("platform", cfg.Platform.String()).Msg("DumpSource: config platform after NewConfig")
		} else {
			internal.Log.Logger.Debug().Msg("DumpSource: config platform is nil after NewConfig")
		}
		e := elemental.NewElemental(cfg)

		imgSource, err := sdkImage.NewSrcFromURI(image)
		if err != nil {
			return err
		}
		if _, err := e.DumpSource(dst, imgSource); err != nil {
			return fmt.Errorf("dumping the source image %s to %s: %w", image, dst, err)
		}

		return nil
	}
}
