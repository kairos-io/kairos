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

package mocks_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/kairos/v4/agent/tests/mocks"
)

var _ = Describe("FakeRunner", func() {
	Describe("MatchMilestones", func() {
		var runner *mocks.FakeRunner

		BeforeEach(func() {
			runner = mocks.NewFakeRunner()
		})

		It("passes when every milestone ran in order", func() {
			_, _ = runner.Run("mkfs.ext4", "/dev/sda2")
			_, _ = runner.Run("tune2fs", "-L", "COS_STATE")
			Expect(runner.MatchMilestones([][]string{{"mkfs.ext4"}, {"tune2fs", "-L"}})).To(Succeed())
		})

		It("names the missing milestone when it comes after the last recorded command", func() {
			_, _ = runner.Run("mkfs.ext4", "/dev/sda2")
			err := runner.MatchMilestones([][]string{{"mkfs.ext4", "/dev/sda2"}, {"tune2fs", "-L", "COS_STATE"}})
			Expect(err).To(MatchError("command 'tune2fs -L COS_STATE' not executed"))
		})

		It("names the missing milestone when nothing ran", func() {
			err := runner.MatchMilestones([][]string{{"tune2fs", "-L", "COS_STATE"}})
			Expect(err).To(MatchError("command 'tune2fs -L COS_STATE' not executed"))
		})

		It("names the first missing milestone, not a later one", func() {
			_, _ = runner.Run("mkfs.ext4", "/dev/sda2")
			_, _ = runner.Run("sync")
			err := runner.MatchMilestones([][]string{{"tune2fs"}, {"sync"}})
			Expect(err).To(MatchError("command 'tune2fs' not executed"))
		})
	})
})
