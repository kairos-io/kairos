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

package mocks

import (
	"errors"

	"github.com/mudler/yip/pkg/schema"
)

type FakeCloudInitRunner struct {
	ExecStages []string
	Error      bool
}

func (ci *FakeCloudInitRunner) Run(stage string, args ...string) error {
	ci.ExecStages = append(ci.ExecStages, stage)
	if ci.Error {
		return errors.New("cloud init failure")
	}
	return nil
}

func (ci *FakeCloudInitRunner) SetModifier(modifier schema.Modifier) {
}

func (ci *FakeCloudInitRunner) Analyze(stage string, args ...string) {}
