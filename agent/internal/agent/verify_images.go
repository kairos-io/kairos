package agent

import (
	"io"
	"os"
	"path/filepath"

	"github.com/kairos-io/kairos/v4/agent/pkg/action"
	"github.com/kairos-io/kairos/v4/agent/pkg/config"
	"github.com/kairos-io/kairos/v4/agent/pkg/constants"
	"github.com/kairos-io/kairos/v4/sdk/collector"
)

// VerifyImages loads the config, which includes the image-checksum.* keys on the
// kernel cmdline, and checks the images in dirs. With no dirs it checks the state
// and recovery partitions mounted by the booted system.
func VerifyImages(w io.Writer, dirs []string) error {
	cfg, err := config.Scan(collector.Directories(constants.GetUserConfigDirs()...), collector.NoLogs)
	if err != nil {
		return err
	}
	if len(dirs) == 0 {
		for _, mnt := range []string{constants.RunningStateDir, constants.RunningRecoveryStateDir} {
			dir := filepath.Join(mnt, "cOS")
			if info, err := os.Stat(dir); err == nil && info.IsDir() {
				dirs = append(dirs, dir)
			}
		}
	}
	return action.VerifyImages(cfg, w, dirs)
}
