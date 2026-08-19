package stages

import (
	"os"
	"path/filepath"

	"github.com/kairos-io/kairos-init/pkg/config"
	"github.com/kairos-io/kairos-sdk/types/logger"
	"github.com/mudler/yip/pkg/schema"
	"github.com/sanity-io/litter"
	"github.com/twpayne/go-vfs/v5"
)

// GetStageExtensions returns the expansions for a given stage
// It loads the extensions from a dir in the filesystem, loads all files and only selects the proper stage to be returned
func GetStageExtensions(stage string, logger logger.KairosLogger) []schema.Stage {
	var data []schema.Stage
	// If extensions are not enabled, return empty
	if !config.DefaultConfig.Extensions {
		return data
	}

	dir := os.Getenv("KAIROS_INIT_STAGE_EXTENSIONS_DIR")
	if dir == "" {
		dir = "/etc/kairos-init/stage-extensions"
	}

	logger.Logger.Debug().Str("stage", stage).Str("dir", dir).Msg("getting stage")

	// Go over all the files in order
	// and load them
	litter.Config.HideZeroValues = true
	litter.Config.HidePrivateFields = false
	_ = vfs.Walk(vfs.OSFS, dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}
		if filepath.Ext(info.Name()) != ".yaml" && filepath.Ext(info.Name()) != ".yml" {
			logger.Logger.Debug().Str("file", path).Msg("skipping file due to file extension")
			return nil
		}

		logger.Logger.Debug().Str("file", path).Msg("loading file")
		d, err := schema.Load(path, vfs.OSFS, schema.FromFile, nil)
		if err != nil {
			logger.Logger.Error().Str("file", path).Err(err).Msg("error loading file")
			return nil
		}
		for _, s := range d.Stages[stage] {
			logger.Logger.Debug().Str("file", path).Msg("found stage, appending data")
			data = append(data, s)
		}
		return nil
	})

	return data
}
