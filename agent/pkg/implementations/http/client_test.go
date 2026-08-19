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

package http_test

import (
	"os"
	"path/filepath"

	"github.com/kairos-io/kairos-agent/v2/pkg/implementations/http"
	"github.com/kairos-io/kairos-sdk/types/logger"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const source = "https://github.com/kairos-io/kairos/releases/download/v2.0.0/core-alpine-arm-rpi-v2.0.0-grype.json"

var _ = Describe("HTTPClient", Label("http"), func() {
	var client *http.Client
	var log logger.KairosLogger
	var destDir string
	BeforeEach(func() {
		client = http.NewClient()
		log = logger.NewNullLogger()
		destDir, _ = os.MkdirTemp("", "elemental-test")
	})
	AfterEach(func() {
		os.RemoveAll(destDir)
	})
	It("Downloads a test file to destination folder", func() {
		// Download a public elemental release
		_, err := os.Stat(filepath.Join(destDir, "core-alpine-arm-rpi-v2.0.0-grype.json"))
		Expect(err).NotTo(BeNil())
		Expect(client.GetURL(log, source, destDir)).To(BeNil())
		_, err = os.Stat(filepath.Join(destDir, "core-alpine-arm-rpi-v2.0.0-grype.json"))
		Expect(err).To(BeNil())
	})
	It("Downloads a test file to some specified destination file", func() {
		// Download a public elemental release
		_, err := os.Stat(filepath.Join(destDir, "testfile"))
		Expect(err).NotTo(BeNil())
		Expect(client.GetURL(log, source, filepath.Join(destDir, "testfile"))).To(BeNil())
		_, err = os.Stat(filepath.Join(destDir, "testfile"))
		Expect(err).To(BeNil())
	})
	It("Fails to download a non existing url", func() {
		source := "http://nonexisting.stuff"
		Expect(client.GetURL(log, source, destDir)).NotTo(BeNil())
	})
	It("Fails to download a broken url", func() {
		source := "scp://23412342341234.wqer.234|@#~ł€@¶|@~#"
		Expect(client.GetURL(log, source, destDir)).NotTo(BeNil())
	})
})
