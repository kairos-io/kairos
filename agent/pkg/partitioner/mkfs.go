/*
Copyright © 2022 SUSE LLC

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

package partitioner

import (
	"fmt"
	"regexp"

	"github.com/kairos-io/kairos-sdk/types/logger"
	"github.com/kairos-io/kairos-sdk/types/runner"
)

type MkfsCall struct {
	fileSystem string
	label      string
	customOpts []string
	dev        string
	runner     runner.Runner
}

func NewMkfsCall(dev string, fileSystem string, label string, runner runner.Runner, customOpts ...string) *MkfsCall {
	return &MkfsCall{dev: dev, fileSystem: fileSystem, label: label, runner: runner, customOpts: customOpts}
}

func (mkfs MkfsCall) buildOptions() ([]string, error) {
	opts := []string{}

	linuxFS, _ := regexp.MatchString("ext[2-4]|xfs", mkfs.fileSystem)
	fatFS, _ := regexp.MatchString("fat|vfat", mkfs.fileSystem)

	switch {
	case linuxFS:
		if mkfs.label != "" {
			opts = append(opts, "-L")
			opts = append(opts, mkfs.label)
		}
		if len(mkfs.customOpts) > 0 {
			opts = append(opts, mkfs.customOpts...)
		}
		opts = append(opts, mkfs.dev)
	case fatFS:
		if mkfs.label != "" {
			opts = append(opts, "-n")
			opts = append(opts, mkfs.label)
		}
		if len(mkfs.customOpts) > 0 {
			opts = append(opts, mkfs.customOpts...)
		}
		opts = append(opts, mkfs.dev)
	default:
		return []string{}, fmt.Errorf("unsupported filesystem: %s", mkfs.fileSystem)
	}
	return opts, nil
}

func (mkfs MkfsCall) Apply() (string, error) {
	opts, err := mkfs.buildOptions()
	if err != nil {
		return "", err
	}
	tool := fmt.Sprintf("mkfs.%s", mkfs.fileSystem)
	out, err := mkfs.runner.Run(tool, opts...)
	return string(out), err
}

// FormatDevice formats a block device with the given parameters
func FormatDevice(logger logger.KairosLogger, runner runner.Runner, device string, fileSystem string, label string, opts ...string) error {
	mkfs := MkfsCall{fileSystem: fileSystem, label: label, customOpts: opts, dev: device, runner: runner}
	out, err := mkfs.Apply()
	if err != nil {
		logger.Logger.Warn().Err(err).Str("output", out).Msg("mkfs failed")
	}
	return err
}
