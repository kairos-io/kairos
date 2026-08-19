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

package action

import (
	"bytes"

	agentConfig "github.com/kairos-io/kairos-agent/v2/pkg/config"
	v1mock "github.com/kairos-io/kairos-agent/v2/tests/mocks"
	"github.com/kairos-io/kairos-sdk/collector"
	"github.com/kairos-io/kairos-sdk/kcrypt"
	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
	sdkLogger "github.com/kairos-io/kairos-sdk/types/logger"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Kcrypt actions", Label("kcrypt"), func() {
	var config *sdkConfig.Config
	var memLog *bytes.Buffer
	var logger sdkLogger.KairosLogger

	// A TPM device path that should never exist so TPM operations fail fast
	const bogusTPMDevice = "/this/does/not/exist/tpmrm0"

	BeforeEach(func() {
		memLog = &bytes.Buffer{}
		logger = sdkLogger.NewBufferLogger(memLog)
		logger.SetLevel("debug")
		config = agentConfig.NewConfig(
			agentConfig.WithLogger(logger),
			agentConfig.WithRunner(v1mock.NewFakeRunner()),
		)
		config.Collector = collector.Config{}
	})

	Describe("resolveNVIndexAndDevice", func() {
		It("prefers the explicit flags over everything", func() {
			config.Collector.Values = collector.ConfigValues{
				"kcrypt": map[string]interface{}{
					"nv_index":   "0x1500001",
					"tpm_device": "/dev/tpmrm9",
				},
			}
			index, device := resolveNVIndexAndDevice(config, "0x1500002", "/dev/tpmrm5")
			Expect(index).To(Equal("0x1500002"))
			Expect(device).To(Equal("/dev/tpmrm5"))
		})
		It("falls back to the config values when no flags are given", func() {
			config.Collector.Values = collector.ConfigValues{
				"kcrypt": map[string]interface{}{
					"nv_index":   "0x1500001",
					"tpm_device": "/dev/tpmrm9",
				},
			}
			index, device := resolveNVIndexAndDevice(config, "", "")
			Expect(index).To(Equal("0x1500001"))
			Expect(device).To(Equal("/dev/tpmrm9"))
		})
		It("uses the default NV index and empty device when nothing is set", func() {
			index, device := resolveNVIndexAndDevice(config, "", "")
			Expect(index).To(Equal(kcrypt.DefaultLocalPassphraseNVIndex))
			Expect(device).To(Equal(""))
		})
		It("ignores non-string config values", func() {
			config.Collector.Values = collector.ConfigValues{
				"kcrypt": map[string]interface{}{
					"nv_index":   123,
					"tpm_device": false,
				},
			}
			index, device := resolveNVIndexAndDevice(config, "", "")
			Expect(index).To(Equal(kcrypt.DefaultLocalPassphraseNVIndex))
			Expect(device).To(Equal(""))
		})
	})

	Describe("resolveCIndex", func() {
		It("prefers the explicit flag", func() {
			config.Collector.Values = collector.ConfigValues{
				"kcrypt": map[string]interface{}{
					"challenger": map[string]interface{}{
						"c_index": "0x1500010",
					},
				},
			}
			Expect(resolveCIndex(config, "0x1500011")).To(Equal("0x1500011"))
		})
		It("falls back to the config value", func() {
			config.Collector.Values = collector.ConfigValues{
				"kcrypt": map[string]interface{}{
					"challenger": map[string]interface{}{
						"c_index": "0x1500010",
					},
				},
			}
			Expect(resolveCIndex(config, "")).To(Equal("0x1500010"))
		})
		It("returns empty when there is no kcrypt config", func() {
			Expect(resolveCIndex(config, "")).To(Equal(""))
		})
		It("returns empty when there is no challenger config", func() {
			config.Collector.Values = collector.ConfigValues{
				"kcrypt": map[string]interface{}{},
			}
			Expect(resolveCIndex(config, "")).To(Equal(""))
		})
	})

	Describe("KcryptReadNV", func() {
		It("fails when the TPM device is not available", func() {
			err := KcryptReadNV(config, "", bogusTPMDevice, "")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to read NV index"))
		})
		It("fails when the NV index is not parseable", func() {
			err := KcryptReadNV(config, "notanindex", bogusTPMDevice, "0x1500010")
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("KcryptCheckNV", func() {
		It("fails when the TPM device is not available", func() {
			err := KcryptCheckNV(config, "", bogusTPMDevice)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("does not exist or is empty"))
		})
	})

	Describe("KcryptCleanup", func() {
		It("does nothing when the NV index can not be read", func() {
			err := KcryptCleanup(config, "", bogusTPMDevice, true)
			Expect(err).ToNot(HaveOccurred())
			Expect(memLog.String()).To(ContainSubstring("appears to be empty or non-existent"))
		})
	})
})
