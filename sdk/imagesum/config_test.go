package imagesum_test

import (
	"github.com/kairos-io/kairos/v4/sdk/imagesum"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v5"
	"github.com/twpayne/go-vfs/v5/vfst"
)

var _ = Describe("policy", func() {
	DescribeTable("ParseAction",
		func(in string, want imagesum.Action, ok bool) {
			got, gotOK := imagesum.ParseAction(in)
			Expect(gotOK).To(Equal(ok))
			Expect(got).To(Equal(want))
		},
		Entry("empty is none", "", imagesum.None, true),
		Entry("none", "none", imagesum.None, true),
		Entry("warn", "warn", imagesum.Warn, true),
		Entry("fail", "fail", imagesum.Fail, true),
		Entry("case and spaces", " FAIL ", imagesum.Fail, true),
		Entry("off is not an action", "off", imagesum.None, false),
		Entry("garbage", "banana", imagesum.None, false),
	)

	It("takes the boot time part from the config", func() {
		yes := true
		c := imagesum.Config{Create: &yes, Verify: imagesum.Warn, Require: &yes}
		Expect(c.Policy()).To(Equal(imagesum.Policy{Verify: imagesum.Warn, Require: true}))
	})

	It("is off when the booleans are unset", func() {
		c := imagesum.Config{}
		Expect(c.CreateEnabled()).To(BeFalse())
		Expect(c.RequireEnabled()).To(BeFalse())
	})

	Describe("policy file", func() {
		var fs vfs.FS
		var cleanup func()
		BeforeEach(func() {
			var err error
			fs, cleanup, err = vfst.NewTestFS(map[string]interface{}{"/state/cOS": &vfst.Dir{Perm: 0o755}})
			Expect(err).ToNot(HaveOccurred())
		})
		AfterEach(func() { cleanup() })

		It("round trips", func() {
			want := imagesum.Policy{Verify: imagesum.Fail, Require: true}
			Expect(imagesum.WritePolicy(fs, "/state/cOS", want)).To(Succeed())
			got, found, err := imagesum.ReadPolicy(fs, "/state/cOS")
			Expect(err).ToNot(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(got).To(Equal(want))
		})
		It("is absent when nothing was written", func() {
			_, found, err := imagesum.ReadPolicy(fs, "/state/cOS")
			Expect(err).ToNot(HaveOccurred())
			Expect(found).To(BeFalse())
		})
		It("removes the file when the policy asks for nothing", func() {
			Expect(imagesum.WritePolicy(fs, "/state/cOS", imagesum.Policy{Verify: imagesum.Warn})).To(Succeed())
			Expect(imagesum.WritePolicy(fs, "/state/cOS", imagesum.Policy{Verify: imagesum.None})).To(Succeed())
			_, found, err := imagesum.ReadPolicy(fs, "/state/cOS")
			Expect(err).ToNot(HaveOccurred())
			Expect(found).To(BeFalse())
			// and removing a file that is not there is fine
			Expect(imagesum.WritePolicy(fs, "/state/cOS", imagesum.Policy{})).To(Succeed())
		})
		It("errors on an unknown action", func() {
			Expect(fs.WriteFile("/state/cOS/"+imagesum.PolicyFile, []byte("verify: sometimes\n"), 0o644)).To(Succeed())
			_, found, err := imagesum.ReadPolicy(fs, "/state/cOS")
			Expect(found).To(BeTrue())
			Expect(err).To(MatchError(ContainSubstring("unknown verify action")))
		})
		It("errors on a file that is not yaml", func() {
			Expect(fs.WriteFile("/state/cOS/"+imagesum.PolicyFile, []byte("::: [\n"), 0o644)).To(Succeed())
			_, _, err := imagesum.ReadPolicy(fs, "/state/cOS")
			Expect(err).To(HaveOccurred())
		})
	})
})
