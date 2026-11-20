/*
Copyright © 2026 Kairos authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package config

import (
	"fmt"

	"github.com/kairos-io/kairos/v4/agent/pkg/implementations/spec"
	sdkConstants "github.com/kairos-io/kairos/v4/sdk/constants"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	sdkImages "github.com/kairos-io/kairos/v4/sdk/types/images"
)

// RecalculateInstallSizes sizes selected sources after downloading installation
// media. It commits the new sizes only when all sources and the target pass.
// No-format installations do not add a target capacity check.
func RecalculateInstallSizes(cfg *sdkConfig.Config, sp *spec.InstallSpec) error {
	planned := *sp
	requested := sp.RequestedSizes()
	if err := setInstallImageSizes(cfg, &planned, requested, false, true); err != nil {
		return err
	}
	// Preserve partition metadata, firmware partitions and any caller settings.
	if sp.Partitions.State != nil {
		state := *sp.Partitions.State
		state.Size = max(requested.StatePartition, planned.Active.Size*2+planned.Passive.Size+1000)
		planned.Partitions.State = &state
	}
	if sp.Partitions.Recovery != nil {
		recovery := *sp.Partitions.Recovery
		recovery.Size = max(requested.RecoveryPartition, planned.Recovery.Size*2+200)
		planned.Partitions.Recovery = &recovery
	}
	if !planned.NoFormat {
		if err := planned.ValidateDiskSize(); err != nil {
			return err
		}
	}
	planned.RecordCalculatedSizes(requested)
	*sp = planned
	return nil
}

// setInstallImageSizes follows file copies on the target instead of statting
// images that have not been deployed yet. Each independent source is measured.
func setInstallImageSizes(cfg *sdkConfig.Config, sp *spec.InstallSpec, requested spec.InstallSizes, pendingISO, strict bool) error {
	images := []*sdkImages.Image{&sp.Active, &sp.Passive, &sp.Recovery}
	minimums := []uint{requested.Active, requested.Passive, requested.Recovery}
	state := make([]uint8, len(images))
	var sizeImage func(int) error
	sizeImage = func(index int) error {
		if state[index] == 2 {
			return nil
		}
		if state[index] == 1 {
			return fmt.Errorf("cyclic installation image copy involving %s", images[index].File)
		}
		state[index] = 1
		img := images[index]
		size := uint(sdkConstants.ImgSize)
		if pendingISO && (index == 0 || index == 2) {
			// These sources will be replaced before any disk operations.
		} else if img.Source == nil || img.Source.IsEmpty() {
			// An unset source historically contributes no size. Sanitize handles
			// missing active sources; callers can still inspect a default plan.
			size = 0
		} else {
			copyIndex := -1
			for n, other := range images {
				if img.Source.IsFile() && other.File != "" && img.Source.Value() == other.File {
					copyIndex = n
					break
				}
			}
			if copyIndex >= 0 {
				if err := sizeImage(copyIndex); err != nil {
					return err
				}
				size = images[copyIndex].Size
			} else {
				measured, err := GetSourceSize(cfg, img.Source)
				if err != nil {
					if strict {
						return fmt.Errorf("sizing installation source %s: %w", img.Source, err)
					}
					cfg.Logger.Warnf("Failed to infer size for image %s: %s", img.File, err)
				} else {
					size = uint(measured)
				}
			}
		}
		img.Size = max(size, minimums[index])
		state[index] = 2
		return nil
	}
	for index := range images {
		if err := sizeImage(index); err != nil {
			return err
		}
	}
	return nil
}
