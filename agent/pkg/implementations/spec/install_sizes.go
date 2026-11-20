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

package spec

// InstallSizes records image and partition sizes in MiB before source sizing.
// Zero means that the source determines the size.
type InstallSizes struct {
	Active, Passive, Recovery         uint
	StatePartition, RecoveryPartition uint
}

type installSizing struct {
	requested, calculated InstallSizes
}

func (i *InstallSpec) currentSizes() InstallSizes {
	sizes := InstallSizes{Active: i.Active.Size, Passive: i.Passive.Size, Recovery: i.Recovery.Size}
	if i.Partitions.State != nil {
		sizes.StatePartition = i.Partitions.State.Size
	}
	if i.Partitions.Recovery != nil {
		sizes.RecoveryPartition = i.Partitions.Recovery.Size
	}
	return sizes
}

// RequestedSizes separates caller minimums from earlier calculated sizes.
// Changes made by a caller after planning become the new minimums, including
// changes that reduce a size.
func (i *InstallSpec) RequestedSizes() InstallSizes {
	current := i.currentSizes()
	if i.sizing == nil {
		return current
	}
	requested := i.sizing.requested
	calculated := i.sizing.calculated
	keepRequest := func(current, calculated, requested uint) uint {
		if current != calculated {
			return current
		}
		return requested
	}
	return InstallSizes{
		Active:            keepRequest(current.Active, calculated.Active, requested.Active),
		Passive:           keepRequest(current.Passive, calculated.Passive, requested.Passive),
		Recovery:          keepRequest(current.Recovery, calculated.Recovery, requested.Recovery),
		StatePartition:    keepRequest(current.StatePartition, calculated.StatePartition, requested.StatePartition),
		RecoveryPartition: keepRequest(current.RecoveryPartition, calculated.RecoveryPartition, requested.RecoveryPartition),
	}
}

// RecordCalculatedSizes remembers the minimums used for a completed size plan.
func (i *InstallSpec) RecordCalculatedSizes(requested InstallSizes) {
	i.sizing = &installSizing{requested: requested, calculated: i.currentSizes()}
}
