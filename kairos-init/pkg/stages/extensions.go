package stages

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/kairos-io/kairos/v4/kairos-init/pkg/config"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
	"github.com/mudler/yip/pkg/schema"
	"github.com/sanity-io/litter"
	"github.com/twpayne/go-vfs/v5"
)

// GetStageExtensions returns the expansions for a given stage
// It loads the extensions from a dir in the filesystem, loads all files and only selects the proper stage to be returned
func GetStageExtensions(stage string, logger logger.KairosLogger) ([]schema.Stage, error) {
	var data []schema.Stage
	// If extensions are not enabled, return empty
	if !config.DefaultConfig.Extensions {
		return data, nil
	}

	dir, explicit := os.LookupEnv("KAIROS_INIT_STAGE_EXTENSIONS_DIR")
	if dir == "" {
		dir = "/etc/kairos-init/stage-extensions"
		explicit = false
	}

	logger.Logger.Debug().Str("stage", stage).Str("dir", dir).Msg("getting stage")

	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		if explicit {
			return nil, fmt.Errorf("stage extensions dir %s does not exist", dir)
		}
		logger.Logger.Warn().Str("stage", stage).Str("dir", dir).Msg("stage extensions are enabled but the extensions dir does not exist")
		return data, nil
	}

	// Go over all the files in order
	// and load them
	litter.Config.HideZeroValues = true
	litter.Config.HidePrivateFields = false
	err := vfs.Walk(vfs.OSFS, dir, func(path string, info os.FileInfo, err error) error {
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
			return fmt.Errorf("loading stage extension %s: %w", path, err)
		}
		for _, s := range d.Stages[stage] {
			logger.Logger.Debug().Str("file", path).Msg("found stage, appending data")
			data = append(data, s)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return data, nil
}
