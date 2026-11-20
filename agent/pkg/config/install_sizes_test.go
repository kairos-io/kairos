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
	"fmt"
	"path/filepath"

	"github.com/kairos-io/kairos/v4/agent/pkg/config"
	"github.com/kairos-io/kairos/v4/agent/pkg/constants"
	"github.com/kairos-io/kairos/v4/agent/pkg/elemental"
	v1 "github.com/kairos-io/kairos/v4/agent/pkg/implementations/spec"
	v1mock "github.com/kairos-io/kairos/v4/agent/tests/mocks"
	"github.com/kairos-io/kairos/v4/sdk/collector"
	sdkConstants "github.com/kairos-io/kairos/v4/sdk/constants"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	sdkImages "github.com/kairos-io/kairos/v4/sdk/types/images"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v5"
	"github.com/twpayne/go-vfs/v5/vfst"
)

var _ = Describe("Classic install source sizing", func() {
	var cfg *sdkConfig.Config
	var fs *vfst.TestFS
	var sized []string

	setConfig := func(settings map[string]interface{}) {
		cfg.Collector = collector.Config{Values: collector.ConfigValues{"install": settings}}
	}
	imageSettings := func(source string, size uint) map[string]interface{} {
		return map[string]interface{}{"source": source, "size": size}
	}
	newSpec := func() *v1.InstallSpec {
		sp, err := config.NewInstallSpec(cfg)
		Expect(err).NotTo(HaveOccurred())
		return sp
	}

	BeforeEach(func() {
		var cleanup func()
		var err error
		fs, cleanup, err = vfst.NewTestFS(nil)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(cleanup)
		logger := sdkLogger.NewNullLogger()
		sized = nil
		extractor := v1mock.NewFakeImageExtractor(logger)
		extractor.SizeSideEffect = func(ref, _ string) (int64, error) {
			sized = append(sized, ref)
			switch ref {
			case "active:latest":
				return 200 * 1024 * 1024, nil // 600 MiB with image overhead
			case "passive:latest":
				return 80 * 1024 * 1024, nil // 300 MiB
			case "recovery:latest":
				return 40 * 1024 * 1024, nil // 200 MiB
			default:
				return 0, fmt.Errorf("unexpected source %s", ref)
			}
		}
		cfg = config.NewConfig(config.WithFs(fs), config.WithLogger(logger), config.WithImageExtractor(extractor))
		cfg.Install.Source = "oci:provisional"
		setConfig(map[string]interface{}{})
	})

	DescribeTable("selects per-image sources before sizing",
		func(key string) {
			setConfig(map[string]interface{}{
				"system":          map[string]interface{}{key: "oci:active"},
				"passive":         map[string]interface{}{key: "oci:passive"},
				"recovery-system": map[string]interface{}{key: "oci:recovery"},
			})
			sp := newSpec()
			Expect(sized).To(ConsistOf("active:latest", "passive:latest", "recovery:latest"))
			Expect(sp.Active.Size).To(Equal(uint(600)))
			Expect(sp.Passive.Size).To(Equal(uint(300)))
			Expect(sp.Recovery.Size).To(Equal(uint(200)))
			Expect(sp.Partitions.State.Size).To(Equal(uint(2500)))
			Expect(sp.Partitions.Recovery.Size).To(Equal(uint(600)))
		},
		Entry("source", "source"), Entry("legacy uri", "uri"),
	)

	It("preserves default sizes when no source is available", func() {
		cfg.Install.Source = ""
		sp := newSpec()
		Expect(sp.Active.Size).To(BeZero())
		Expect(sp.Passive.Size).To(BeZero())
		Expect(sp.Recovery.Size).To(BeZero())
		Expect(sp.Partitions.State.Size).To(Equal(uint(1000)))
		Expect(sp.Partitions.Recovery.Size).To(Equal(uint(200)))
	})

	It("sizes caller default sources and treats caller sizes as minimums", func() {
		cfg.Install.Active = sdkImages.Image{URI: "oci:active", Size: 700}
		cfg.Install.Passive = sdkImages.Image{Source: sdkImages.NewDockerSrc("passive:latest"), Size: 100}
		cfg.Install.Recovery = sdkImages.Image{URI: "oci:recovery", Size: 250}
		sp := newSpec()
		Expect(sized).To(ConsistOf("active:latest", "passive:latest", "recovery:latest"))
		Expect(sp.Active.Size).To(Equal(uint(700)))
		Expect(sp.Passive.Size).To(Equal(uint(300)))
		Expect(sp.Recovery.Size).To(Equal(uint(250)))
	})

	It("ignores malformed overridden URIs and the top-level fallback", func() {
		cfg.Install.Source = "%invalid"
		settings := map[string]interface{}{}
		for key, source := range map[string]string{"system": "oci:active", "passive": "oci:passive", "recovery-system": "oci:recovery"} {
			settings[key] = map[string]interface{}{"source": source, "uri": map[string]interface{}{"invalid": true}}
		}
		setConfig(settings)
		Expect(newSpec().Active.Size).To(Equal(uint(600)))
		Expect(sized).To(HaveLen(3))
	})

	It("uses the top-level and live recovery defaults independently", func() {
		cfg.Install.Source = "oci:active"
		path := filepath.Join(constants.LiveDir, constants.RecoverySquashFile)
		Expect(vfs.MkdirAll(fs, filepath.Dir(path), 0755)).To(Succeed())
		Expect(fs.WriteFile(path, []byte("recovery"), 0644)).To(Succeed())
		sp := newSpec()
		Expect(sp.Active.Size).To(Equal(uint(600)))
		Expect(sp.Passive.Size).To(Equal(uint(600)))
		Expect(sp.Recovery.Size).To(Equal(uint(100)))
		Expect(sp.Partitions.Recovery.Size).To(Equal(uint(400)))
	})

	It("propagates minimums through copies without statting target images", func() {
		activeFile := filepath.Join(constants.StateDir, "cOS", constants.ActiveImgFile)
		passiveFile := filepath.Join(constants.StateDir, "cOS", constants.PassiveImgFile)
		setConfig(map[string]interface{}{
			"system":          imageSettings("oci:active", 700),
			"passive":         imageSettings("file:"+activeFile, 800),
			"recovery-system": imageSettings("file:"+passiveFile, 100),
		})
		sp := newSpec()
		Expect(sp.Active.Size).To(Equal(uint(700)))
		Expect(sp.Passive.Size).To(Equal(uint(800)))
		Expect(sp.Recovery.Size).To(Equal(uint(800)))
		Expect(sp.Partitions.State.Size).To(Equal(uint(3200)))
		Expect(sp.Partitions.Recovery.Size).To(Equal(uint(1800)))
		Expect(sized).To(Equal([]string{"active:latest"}))
	})

	It("defers replaced ISO sources but sizes independent passive sources", func() {
		setConfig(map[string]interface{}{
			"iso":             "file:/install.iso",
			"system":          imageSettings("%invalid", 0),
			"recovery-system": map[string]interface{}{"source": []string{"invalid"}, "uri": "%invalid"},
			"passive":         imageSettings("oci:passive", 400),
		})
		sp := newSpec()
		Expect(sized).To(Equal([]string{"passive:latest"}))
		Expect(sp.Passive.Size).To(Equal(uint(400)))
		Expect(sp.Active.Size).To(Equal(uint(sdkConstants.ImgSize)))
	})

	It("retains configured image and partition minimums when sources shrink", func() {
		setConfig(map[string]interface{}{
			"system": imageSettings("oci:active", 350),
			"partitions": map[string]interface{}{
				"state":    map[string]interface{}{"size": 2000},
				"recovery": map[string]interface{}{"size": 1000},
			},
		})
		sp := newSpec()
		sp.Active.Source = sdkImages.NewDockerSrc("recovery:latest")
		Expect(config.RecalculateInstallSizes(cfg, sp)).To(Succeed())
		Expect(sp.Active.Size).To(Equal(uint(350)))
		Expect(sp.Passive.Size).To(Equal(uint(350)))
		Expect(sp.Recovery.Size).To(Equal(uint(350)))
		Expect(sp.Partitions.State.Size).To(Equal(uint(2050)))
		Expect(sp.Partitions.Recovery.Size).To(Equal(uint(1000)))
	})

	DescribeTable("honors caller changes to an independent passive minimum",
		func(minimum, expected uint) {
			setConfig(map[string]interface{}{"system": imageSettings("oci:active", 0), "passive": imageSettings("oci:passive", 800)})
			sp := newSpec()
			sp.Passive.Size = minimum
			sp.Active.Source = sdkImages.NewDockerSrc("recovery:latest")
			Expect(config.RecalculateInstallSizes(cfg, sp)).To(Succeed())
			Expect(sp.Active.Size).To(Equal(uint(200)))
			Expect(sp.Passive.Size).To(Equal(expected))
		},
		Entry("smaller", uint(400), uint(400)), Entry("larger", uint(900), uint(900)), Entry("below the source", uint(100), uint(300)),
	)

	It("keeps passive copies linked to recovery when an ISO changes its format", func() {
		setConfig(map[string]interface{}{"system": imageSettings("oci:active", 0)})
		sp := newSpec()
		sp.Passive.Source = sdkImages.NewFileSrc(sp.Recovery.File)
		Expect(vfs.MkdirAll(fs, "/download/iso", 0755)).To(Succeed())
		Expect(vfs.MkdirAll(fs, "/download/rootfs", 0755)).To(Succeed())
		Expect(fs.WriteFile("/download/rootfs/os", []byte("root"), 0644)).To(Succeed())
		Expect(fs.WriteFile(filepath.Join("/download/iso", constants.RecoverySquashFile), []byte("recovery"), 0644)).To(Succeed())
		e := elemental.NewElemental(cfg)
		Expect(e.UpdateSourcesFormDownloadedISO("/download", &sp.Active, &sp.Recovery)).To(Succeed())
		sp.SetRecoveryImageFile()
		Expect(config.RecalculateInstallSizes(cfg, sp)).To(Succeed())
		Expect(sp.Passive.Source.Value()).To(Equal(sp.Recovery.File))
		Expect(sp.Recovery.File).To(HaveSuffix(constants.RecoverySquashFile))
		Expect(sp.Active.Size).To(Equal(uint(100)))
		Expect(sp.Recovery.Size).To(Equal(uint(100)))
		Expect(sp.Passive.Size).To(Equal(uint(100)))
	})

	It("leaves sizes unchanged when a selected source cannot be measured", func() {
		setConfig(map[string]interface{}{"system": imageSettings("oci:active", 0)})
		sp := newSpec()
		sp.Recovery.Source = sdkImages.NewFileSrc("/missing")
		before := *sp
		Expect(config.RecalculateInstallSizes(cfg, sp)).NotTo(Succeed())
		Expect(*sp).To(Equal(before))
	})

	It("still rejects a malformed selected source", func() {
		setConfig(map[string]interface{}{"system": imageSettings("%invalid", 0)})
		_, err := config.NewInstallSpec(cfg)
		Expect(err).To(HaveOccurred())
	})
})
