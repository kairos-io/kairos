package state

import (
	goruntime "runtime"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("OS architecture", func() {
	Describe("resolveArch", func() {
		It("keeps what sysinfo detected", func() {
			Expect(resolveArch("amd64", "arm64")).To(Equal("amd64"))
			Expect(resolveArch("i386", "amd64")).To(Equal("i386"))
		})

		It("falls back to GOARCH on the arches sysinfo cannot detect", func() {
			Expect(resolveArch("", "arm64")).To(Equal("arm64"))
			Expect(resolveArch("", "riscv64")).To(Equal("riscv64"))
			Expect(resolveArch("", "amd64")).To(Equal("amd64"))
		})

		It("spells 32 bit x86 the way sysinfo spells it", func() {
			Expect(resolveArch("", "386")).To(Equal("i386"))
		})
	})

	Describe("detectSystem", func() {
		It("always names an architecture", func() {
			r := &Runtime{}
			detectSystem(r)

			Expect(r.System.OS.Architecture).To(Equal(resolveArch("", goruntime.GOARCH)))
		})
	})
})
