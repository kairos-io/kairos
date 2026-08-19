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

package cloudinit

import (
	sdkLogger "github.com/kairos-io/kairos-sdk/types/logger"
	sdkRunner "github.com/kairos-io/kairos-sdk/types/runner"
	"github.com/mudler/yip/pkg/executor"
	"github.com/mudler/yip/pkg/plugins"
	"github.com/mudler/yip/pkg/schema"
	"github.com/twpayne/go-vfs/v5"
)

type YipCloudInitRunner struct {
	exec    executor.Executor
	fs      vfs.FS
	console plugins.Console
}

// NewYipCloudInitRunner returns a default yip cloud init executor with the Elemental plugin set.
// It accepts a logger which is used inside the runner.
func NewYipCloudInitRunner(l sdkLogger.KairosLogger, r sdkRunner.Runner, fs vfs.FS) *YipCloudInitRunner {
	exec := executor.NewExecutor(executor.WithLogger(l))
	return &YipCloudInitRunner{
		exec: exec, fs: fs,
		console: newCloudInitConsole(l, r),
	}
}

func (ci YipCloudInitRunner) Run(stage string, args ...string) error {
	return ci.exec.Run(stage, ci.fs, ci.console, args...)
}

func (ci *YipCloudInitRunner) SetModifier(m schema.Modifier) {
	ci.exec.Modifier(m)
}

// Useful for testing purposes
func (ci *YipCloudInitRunner) SetFs(fs vfs.FS) {
	ci.fs = fs
}

func (ci *YipCloudInitRunner) Analyze(stage string, args ...string) {
	ci.exec.Analyze(stage, vfs.OSFS, ci.console, args...)
}
