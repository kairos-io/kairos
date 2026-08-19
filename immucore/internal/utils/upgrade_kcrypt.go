package utils

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/gofrs/uuid"
	"github.com/kairos-io/kairos-sdk/ghw"
	"github.com/kairos-io/kairos-sdk/utils"
)

// UpgradeKcryptPartitions will try check for the uuid of the persistent partition and upgrade its uuid.
func UpgradeKcryptPartitions() error {
	// Generate the predictable UUID
	persistentUUID := uuid.NewV5(uuid.NamespaceURL, "COS_PERSISTENT")
	// Check if there are any LUKS partitions, otherwise ignore
	disks := ghw.GetDisks(ghw.NewPaths(""), &KLog)

	for _, disk := range disks {
		for _, p := range disk.Partitions {
			if p.FS == "crypto_LUKS" {
				// Check against known partition label on persistent
				KLog.Logger.Debug().Str("label", p.FilesystemLabel).Str("dev", p.Name).Msg("found luks partition")
				if p.FilesystemLabel == "persistent" {
					// Get current UUID
					volumeUUID, err := utils.SH(fmt.Sprintf("cryptsetup luksUUID %s", filepath.Join("/dev", p.Name)))
					if err != nil {
						KLog.Logger.Err(err).Send()
						return err
					}
					volumeUUID = strings.TrimSpace(volumeUUID)
					volumeUUIDParsed, err := uuid.FromString(volumeUUID)
					KLog.Logger.Debug().Interface("volumeUUID", volumeUUIDParsed).Send()
					KLog.Logger.Debug().Interface("persistentUUID", persistentUUID).Send()
					if err != nil {
						KLog.Logger.Err(err).Send()
						return err
					}

					// Check to see if it's the same already to not do anything
					if volumeUUIDParsed.String() != persistentUUID.String() {
						KLog.Logger.Debug().Str("old", volumeUUIDParsed.String()).Str("new", persistentUUID.String()).Msg("Uuid is different, updating")
						out, err := utils.SH(fmt.Sprintf("cryptsetup luksUUID -q --uuid %s %s", persistentUUID, filepath.Join("/dev", p.Name)))
						if err != nil {
							KLog.Logger.Err(err).Str("out", out).Msg("Updating uuid failed")
							return err
						}
					} else {
						KLog.Logger.Debug().Msg("UUIDs are the same, not updating")
					}
				}
			}
		}
	}

	return nil
}
