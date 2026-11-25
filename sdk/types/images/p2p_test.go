package images_test

import (
	"strings"

	"github.com/kairos-io/kairos/v4/sdk/types/images"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
)

var _ = Describe("p2p source (EXPERIMENTAL)", Label("types", "common"), func() {
	sum := strings.Repeat("ab", 32)

	DescribeTable("parses valid URIs",
		func(uri, value string, target images.P2PTarget) {
			src, err := images.NewSrcFromURI(uri)
			Expect(err).ToNot(HaveOccurred())
			Expect(src.IsP2P()).To(BeTrue())
			Expect(src.IsRawImage()).To(BeTrue())
			Expect(src.IsFile()).To(BeFalse())
			Expect(src.IsDocker()).To(BeFalse())
			Expect(src.IsEmpty()).To(BeFalse())
			Expect(src.Value()).To(Equal(value))
			got, err := src.P2PTarget()
			Expect(err).ToNot(HaveOccurred())
			Expect(got).To(Equal(target))
			Expect(got.Discover()).To(Equal(len(target.Peers) == 0))
			// the string form parses back to the same source
			again, err := images.NewSrcFromURI(src.String())
			Expect(err).ToNot(HaveOccurred())
			Expect(again.String()).To(Equal(src.String()))
		},
		Entry("discovery", "p2p://active", "active", images.P2PTarget{Image: "active"}),
		Entry("discovery of recovery", "p2p://recovery", "recovery", images.P2PTarget{Image: "recovery"}),
		Entry("one peer, default port", "p2p://10.0.0.2/active", "10.0.0.2:42069/active",
			images.P2PTarget{Peers: []string{"10.0.0.2:42069"}, Image: "active"}),
		Entry("peers with ports", "p2p://10.0.0.2,node3:4000/passive", "10.0.0.2:42069,node3:4000/passive",
			images.P2PTarget{Peers: []string{"10.0.0.2:42069", "node3:4000"}, Image: "passive"}),
		Entry("a sha256 pin", "p2p://10.0.0.2/active?sha256="+strings.ToUpper(sum), "10.0.0.2:42069/active?sha256="+sum,
			images.P2PTarget{Peers: []string{"10.0.0.2:42069"}, Image: "active", SHA256: sum}),
		Entry("a pin with discovery", "p2p://active?sha256="+sum, "active?sha256="+sum,
			images.P2PTarget{Image: "active", SHA256: sum}),
		Entry("V5: a port on every peer, not only the last", "p2p://10.0.0.2:4000,10.0.0.3:4001,10.0.0.4/active", "10.0.0.2:4000,10.0.0.3:4001,10.0.0.4:42069/active",
			images.P2PTarget{Peers: []string{"10.0.0.2:4000", "10.0.0.3:4001", "10.0.0.4:42069"}, Image: "active"}),
		Entry("V5: a port on the first peer only", "p2p://10.0.0.2:4000,10.0.0.3/passive?sha256="+sum, "10.0.0.2:4000,10.0.0.3:42069/passive?sha256="+sum,
			images.P2PTarget{Peers: []string{"10.0.0.2:4000", "10.0.0.3:42069"}, Image: "passive", SHA256: sum}),
		Entry("the manifest pin", "p2p://10.0.0.2/active?pin="+strings.ToUpper(sum), "10.0.0.2:42069/active?pin="+sum,
			images.P2PTarget{Peers: []string{"10.0.0.2:42069"}, Image: "active", Pin: sum}),
		Entry("both pins and a size", "p2p://active?pin="+sum+"&sha256="+strings.Repeat("cd", 32)+"&size=100", "active?pin="+sum+"&sha256="+strings.Repeat("cd", 32)+"&size=100",
			images.P2PTarget{Image: "active", Pin: sum, SHA256: strings.Repeat("cd", 32), Size: 100}),
		Entry("a size with the manifest pin alone", "p2p://active?pin="+sum+"&size=100", "active?pin="+sum+"&size=100",
			images.P2PTarget{Image: "active", Pin: sum, Size: 100}),
		Entry("a size pin", "p2p://10.0.0.2/active?sha256="+sum+"&size=268435456", "10.0.0.2:42069/active?sha256="+sum+"&size=268435456",
			images.P2PTarget{Peers: []string{"10.0.0.2:42069"}, Image: "active", SHA256: sum, Size: 268435456}),
		Entry("the same peer twice is one", "p2p://10.0.0.2,10.0.0.2:42069/active", "10.0.0.2:42069/active",
			images.P2PTarget{Peers: []string{"10.0.0.2:42069"}, Image: "active"}),
	)

	DescribeTable("rejects what is not a p2p source",
		func(uri string) {
			_, err := images.NewSrcFromURI(uri)
			Expect(err).To(HaveOccurred())
		},
		Entry("an unknown image", "p2p://10.0.0.2/persistent"),
		Entry("no image", "p2p://10.0.0.2"),
		Entry("a traversal", "p2p://10.0.0.2/../etc"),
		Entry("a path below the image", "p2p://10.0.0.2/active/x"),
		Entry("a port out of range", "p2p://10.0.0.2:99999/active"),
		Entry("a port that is not a number", "p2p://10.0.0.2:x/active"),
		Entry("ipv6 without brackets", "p2p://fe80::1/active"),
		Entry("a host starting with a dash", "p2p://-evil/active"),
		Entry("a user", "p2p://root@10.0.0.2/active"),
		Entry("a bad pin", "p2p://10.0.0.2/active?sha256=zz"),
		Entry("a short pin", "p2p://10.0.0.2/active?sha256=abcd"),
		Entry("an unknown option", "p2p://10.0.0.2/active?insecure=1"),
		Entry("W5: a size that is not valid url encoding", "p2p://10.0.0.2/active?sha256="+sum+"&size=%zz"),
		Entry("W5: an option given twice", "p2p://10.0.0.2/active?sha256="+sum+"&sha256="+sum),
		Entry("W5: the pin given twice", "p2p://10.0.0.2/active?pin="+sum+"&pin="+strings.Repeat("ab", 32)),
		Entry("W5: an option without a value", "p2p://10.0.0.2/active?sha256="),
		Entry("W5: an empty pin", "p2p://10.0.0.2/active?pin="),
		Entry("a bad pin", "p2p://10.0.0.2/active?pin=abcd"),
		Entry("a size without a pin", "p2p://10.0.0.2/active?size=100"),
		Entry("a zero size", "p2p://10.0.0.2/active?sha256="+sum+"&size=0"),
		Entry("a size that is not a number", "p2p://10.0.0.2/active?sha256="+sum+"&size=abc"),
		Entry("a bad port in the middle", "p2p://10.0.0.2:99999,10.0.0.3/active"),
		Entry("a manifest pin (not supported)", "p2p://10.0.0.2/active?manifest="+sum),
		Entry("a key in the uri", "p2p://10.0.0.2/active?key=ed25519:AAAA"),
		Entry("a fragment", "p2p://10.0.0.2/active#x"),
	)

	It("is not an ssh or a file source", func() {
		src := images.NewP2PSrc(images.P2PTarget{Image: "active"})
		Expect(src.String()).To(Equal("p2p://active"))
		_, err := images.NewFileSrc("/x").P2PTarget()
		Expect(err).To(HaveOccurred())
	})

	It("round trips through yaml as the source of an image", func() {
		var img images.Image
		Expect(yaml.Unmarshal([]byte("source: p2p://10.0.0.2/active?sha256="+sum+"\n"), &img)).To(Succeed())
		Expect(img.Source.IsP2P()).To(BeTrue())
		out, err := yaml.Marshal(img)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(out)).To(ContainSubstring("p2p://10.0.0.2:42069/active?sha256=" + sum))
		Expect(yaml.Unmarshal([]byte("source: p2p://nonsense/x\n"), &img)).ToNot(Succeed())
	})

	It("keeps every other source type working", func() {
		for _, uri := range []string{"oci:quay.io/kairos/x:1", "file:/a.img", "dir:/x", "ocifile:/a.tar"} {
			src, err := images.NewSrcFromURI(uri)
			Expect(err).ToNot(HaveOccurred(), uri)
			Expect(src.IsP2P()).To(BeFalse(), uri)
		}
		f, _ := images.NewSrcFromURI("file:/a.img")
		Expect(f.IsRawImage()).To(BeTrue())
		d, _ := images.NewSrcFromURI("dir:/x")
		Expect(d.IsRawImage()).To(BeFalse())
	})

	It("announces the node unless told not to", func() {
		Expect(images.P2POptions{}.DisableAnnounce).To(BeFalse())
	})
})
