/*
Copyright © 2021 SUSE LLC

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

package images_test

import (
	"github.com/kairos-io/kairos-sdk/types/images"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Types", Label("types", "common"), func() {
	Describe("Source", func() {
		It("initiates each type as expected", func() {
			o := &images.ImageSource{}
			Expect(o.Value()).To(Equal(""))
			Expect(o.IsDir()).To(BeFalse())
			Expect(o.IsDocker()).To(BeFalse())
			Expect(o.IsFile()).To(BeFalse())
			o = images.NewDirSrc("dir")
			Expect(o.IsDir()).To(BeTrue())
			o = images.NewFileSrc("file")
			Expect(o.IsFile()).To(BeTrue())
			o = images.NewOCIFileSrc("file")
			Expect(o.IsOCIFile()).To(BeTrue())
			o = images.NewDockerSrc("image")
			Expect(o.IsDocker()).To(BeTrue())
			o = images.NewEmptySrc()
			Expect(o.IsEmpty()).To(BeTrue())
			o, err := images.NewSrcFromURI("registry.company.org/image")
			Expect(o.IsDocker()).To(BeTrue())
			Expect(err).ShouldNot(HaveOccurred())
			Expect(o.Value()).To(Equal("registry.company.org/image:latest"))
			o, err = images.NewSrcFromURI("oci://registry.company.org/image:tag")
			Expect(o.IsDocker()).To(BeTrue())
			Expect(err).ShouldNot(HaveOccurred())
			Expect(o.Value()).To(Equal("registry.company.org/image:tag"))
		})
		It("unmarshals each type as expected", func() {
			o := images.NewEmptySrc()
			_, err := o.CustomUnmarshal("docker://some/image")
			Expect(err).ShouldNot(HaveOccurred())
			Expect(o.IsDocker()).To(BeTrue())
			_, err = o.CustomUnmarshal("dir:///some/absolute/path")
			Expect(err).ShouldNot(HaveOccurred())
			Expect(o.IsDir()).To(BeTrue())
			Expect(o.Value() == "/some/absolute/path").To(BeTrue())
			_, err = o.CustomUnmarshal("file://some/relative/path")
			Expect(err).ShouldNot(HaveOccurred())
			Expect(o.IsFile()).To(BeTrue())
			Expect(o.Value() == "some/relative/path").To(BeTrue())

			// Opaque URI
			_, err = o.CustomUnmarshal("docker:some/image")
			Expect(err).ShouldNot(HaveOccurred())
			Expect(o.IsDocker()).To(BeTrue())

			// No scheme is parsed as an image reference and
			// defaults to latest tag if none
			_, err = o.CustomUnmarshal("registry.company.org/my/image")
			Expect(err).ShouldNot(HaveOccurred())
			Expect(o.IsDocker()).To(BeTrue())
			Expect(o.Value()).To(Equal("registry.company.org/my/image:latest"))

			_, err = o.CustomUnmarshal("registry.company.org/my/image:tag")
			Expect(err).ShouldNot(HaveOccurred())
			Expect(o.IsDocker()).To(BeTrue())
			Expect(o.Value()).To(Equal("registry.company.org/my/image:tag"))
		})
		It("convertion to string URI works are expected", func() {
			o := images.NewDirSrc("/some/dir")
			Expect(o.IsDir()).To(BeTrue())
			Expect(o.String()).To(Equal("dir:///some/dir"))
			o = images.NewFileSrc("filename")
			Expect(o.IsFile()).To(BeTrue())
			Expect(o.String()).To(Equal("file://filename"))
			o = images.NewDockerSrc("container/image")
			Expect(o.IsDocker()).To(BeTrue())
			Expect(o.String()).To(Equal("oci://container/image"))
			o = images.NewEmptySrc()
			Expect(o.IsEmpty()).To(BeTrue())
			Expect(o.String()).To(Equal(""))
			o, err := images.NewSrcFromURI("registry.company.org/image")
			Expect(o.IsDocker()).To(BeTrue())
			Expect(err).ShouldNot(HaveOccurred())
			Expect(o.String()).To(Equal("oci://registry.company.org/image:latest"))
		})
		It("fails to unmarshal non string types", func() {
			o := images.NewEmptySrc()
			_, err := o.CustomUnmarshal(map[string]string{})
			Expect(err).Should(HaveOccurred())
		})
		It("fails to unmarshal unknown scheme and invalid image reference", func() {
			o := images.NewEmptySrc()
			_, err := o.CustomUnmarshal("scheme://some.uri.org")
			Expect(err).Should(HaveOccurred())
		})
		It("fails to unmarshal invalid uri", func() {
			o := images.NewEmptySrc()
			_, err := o.CustomUnmarshal("jp#afs://insanity")
			Expect(err).Should(HaveOccurred())
		})
	})
})
