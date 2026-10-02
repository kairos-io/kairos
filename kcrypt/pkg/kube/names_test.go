package kube_test

import (
	"testing"

	"github.com/kairos-io/kairos/v4/kcrypt/pkg/kube"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/util/validation"
)

func TestKube(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Kcrypt kube suite")
}

var _ = Describe("SafeKubeName", func() {
	// The API server validates a Secret name with ValidateSecretName, which is
	// NameIsDNSSubdomain, which is IsDNS1123Subdomain. Assert against that rather
	// than against a copy of the rule, so the spec cannot drift from Kubernetes.
	expectValidName := func(in string) string {
		out := kube.SafeKubeName(in)
		ExpectWithOffset(1, validation.IsDNS1123Subdomain(out)).To(BeEmpty(),
			"SafeKubeName(%q) returned %q, which Kubernetes rejects", in, out)
		return out
	}

	It("lower cases a name that is otherwise already valid", func() {
		// The generated name is "<volume>-<partition label>". Kairos' own labels
		// carry an underscore, which forces the sanitizing path, but an extra
		// encrypted partition can be labelled "DATA".
		Expect(expectValidName("tofu-a1b2c3d4-DATA")).To(Equal("tofu-a1b2c3d4-data"))
		Expect(expectValidName("tofu-a1b2c3d4-Data")).To(Equal("tofu-a1b2c3d4-data"))
		Expect(expectValidName("MyVolume-MYDATA")).To(Equal("myvolume-mydata"))
	})

	It("keeps lower casing the names that already went through the sanitizer", func() {
		Expect(expectValidName("tofu-a1b2c3d4-COS_PERSISTENT")).To(Equal("tofu-a1b2c3d4-cos-persistent"))
		Expect(expectValidName("tofu-a1b2c3d4-COS_OEM")).To(Equal("tofu-a1b2c3d4-cos-oem"))
	})

	It("leaves a valid lower case name alone", func() {
		Expect(expectValidName("tofu-a1b2c3d4-cos-persistent")).To(Equal("tofu-a1b2c3d4-cos-persistent"))
	})

	It("truncates a long name and keeps it valid", func() {
		long := "tofu-a1b2c3d4-" + "abcdefghij0123456789abcdefghij0123456789abcdefghij0123456789"
		out := expectValidName(long)
		Expect(len(out)).To(BeNumerically("<=", 63))
		Expect(out).NotTo(Equal(long))
	})

	It("is stable, so a name it produced once survives a second pass", func() {
		once := expectValidName("tofu-a1b2c3d4-DATA")
		Expect(kube.SafeKubeName(once)).To(Equal(once))
	})
})

var _ = Describe("IsValidKubeName", func() {
	It("rejects an uppercase name, which Kubernetes does not accept", func() {
		Expect(kube.IsValidKubeName("mydata")).To(BeTrue())
		Expect(kube.IsValidKubeName("MYDATA")).To(BeFalse())
		Expect(kube.IsValidKubeName("myData")).To(BeFalse())
	})

	It("rejects the characters an RFC 1123 subdomain has no room for", func() {
		Expect(kube.IsValidKubeName("my_data")).To(BeFalse())
		Expect(kube.IsValidKubeName("-mydata")).To(BeFalse())
		Expect(kube.IsValidKubeName("mydata-")).To(BeFalse())
		Expect(kube.IsValidKubeName("")).To(BeFalse())
	})
})
