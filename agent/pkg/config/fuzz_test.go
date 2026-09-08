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

package config_test

import (
	"testing"

	. "github.com/kairos-io/kairos/v4/agent/pkg/config"
)

// FuzzFilterKeys exercises the YAML-to-Config unmarshal every #cloud-config
// (and kernel cmdline fragment) passes through during collector.Scan at
// boot. A panic here is a denial-of-service on that path, not just a bad
// error message.
func FuzzFilterKeys(f *testing.F) {
	seeds := []string{
		"",
		"install:\n  device: /dev/sda",
		"options:\n  foo: bar",
		"env:\n  - FOO=bar",
		"bundles:\n  - targets:\n      - foo",
		"grub_options:\n  foo: bar",
		"logs:\n  level: debug",
		"not even yaml: [",
		"a: &a [*a]",
		"install: null",
		"options: null",
		"env: null",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, source string) {
		_, _ = FilterKeys([]byte(source))
	})
}
