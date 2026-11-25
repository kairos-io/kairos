package images_test

import (
	"github.com/kairos-io/kairos/v4/sdk/types/images"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
)

var _ = Describe("ssh source", Label("types", "common"), func() {
	DescribeTable("parses valid URIs",
		func(uri, value string, target images.SSHTarget) {
			src, err := images.NewSrcFromURI(uri)
			Expect(err).ToNot(HaveOccurred())
			Expect(src.IsSSH()).To(BeTrue())
			Expect(src.IsRawImage()).To(BeTrue())
			Expect(src.IsFile()).To(BeFalse())
			Expect(src.IsDocker()).To(BeFalse())
			Expect(src.IsEmpty()).To(BeFalse())
			Expect(src.Value()).To(Equal(value))
			got, err := src.SSHTarget()
			Expect(err).ToNot(HaveOccurred())
			Expect(got).To(Equal(target))
			// the string form parses back to the same source
			again, err := images.NewSrcFromURI(src.String())
			Expect(err).ToNot(HaveOccurred())
			Expect(again.String()).To(Equal(src.String()))
		},
		Entry("no user, the current one applies", "ssh://node1/active", "node1/active",
			images.SSHTarget{Host: "node1", Path: "/active"}),
		Entry("booted image, default port", "ssh://root@node1", "root@node1",
			images.SSHTarget{User: "root", Host: "node1"}),
		Entry("trailing slash is the booted image", "ssh://root@node1/", "root@node1",
			images.SSHTarget{User: "root", Host: "node1"}),
		Entry("port and active", "ssh://kairos@10.0.0.5:2222/active", "kairos@10.0.0.5:2222/active",
			images.SSHTarget{User: "kairos", Host: "10.0.0.5", Port: "2222", Path: "/active"}),
		Entry("passive", "ssh://root@node1/passive", "root@node1/passive",
			images.SSHTarget{User: "root", Host: "node1", Path: "/passive"}),
		Entry("explicit file", "ssh://root@node1/run/initramfs/cos-state/cOS/active.img", "root@node1/run/initramfs/cos-state/cOS/active.img",
			images.SSHTarget{User: "root", Host: "node1", Path: "/run/initramfs/cos-state/cOS/active.img"}),
		Entry("ipv6", "ssh://root@[fe80::1]:22/active", "root@[fe80::1]:22/active",
			images.SSHTarget{User: "root", Host: "fe80::1", Port: "22", Path: "/active"}),
	)

	DescribeTable("rejects invalid URIs",
		func(uri, msg string) {
			_, err := images.NewSrcFromURI(uri)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(msg))
		},
		Entry("option looking user", "ssh://-oProxyCommand=x@node1", "user is not valid"),
		Entry("option looking host", "ssh://root@-oProxyCommand=x", "host is not valid"),
		Entry("password", "ssh://root:hunter2@node1", "password"),
		Entry("empty password", "ssh://root:@node1", "password"),
		Entry("no host", "ssh://root@/active", "host is required"),
		Entry("relative path", "ssh://root@node1/active/../x", "clean path"),
		Entry("double slash", "ssh://root@node1//etc/x", "clean path"),
		Entry("control character", "ssh://root@node1/a%0Ab", "clean path"),
		Entry("trailing slash on a file", "ssh://root@node1/a/b/", "clean path"),
		Entry("bad port", "ssh://root@node1:99999", "port"),
		Entry("query", "ssh://root@node1/active?x=1", "query"),
		Entry("no slashes", "ssh:root@node1", "use ssh://"),
	)

	It("does not leak the password in the error", func() {
		_, err := images.NewSrcFromURI("ssh://root:hunter2@node1")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).ToNot(ContainSubstring("hunter2"))
	})

	It("builds the address with the default port", func() {
		Expect(images.SSHTarget{Host: "node1"}.Addr()).To(Equal("node1:22"))
		Expect(images.SSHTarget{Host: "fe80::1", Port: "2200"}.Addr()).To(Equal("[fe80::1]:2200"))
	})

	It("round trips through yaml", func() {
		var v struct {
			Source *images.ImageSource `yaml:"source"`
		}
		Expect(yaml.Unmarshal([]byte("source: ssh://root@node1:2222/active\n"), &v)).To(Succeed())
		Expect(v.Source.IsSSH()).To(BeTrue())
		out, err := yaml.Marshal(v)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(out)).To(ContainSubstring("ssh://root@node1:2222/active"))
	})

	It("is not an ssh target when it is another source", func() {
		_, err := images.NewFileSrc("/x").SSHTarget()
		Expect(err).To(HaveOccurred())
		Expect(images.NewFileSrc("/x").IsRawImage()).To(BeTrue())
		Expect(images.NewDirSrc("/x").IsRawImage()).To(BeFalse())
	})
})
