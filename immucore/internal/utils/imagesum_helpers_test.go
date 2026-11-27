package utils_test

import (
	sdkFs "github.com/kairos-io/kairos/v4/sdk/types/fs"
	"github.com/twpayne/go-vfs/v5"
)

func vfsOS() sdkFs.KairosFS { return vfs.OSFS }
