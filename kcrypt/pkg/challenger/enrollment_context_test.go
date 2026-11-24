package challenger

import (
	"context"

	"github.com/go-logr/logr"
	keyserverv1alpha1 "github.com/kairos-io/kairos/v4/kcrypt/api/v1alpha1"
	"github.com/kairos-io/kairos/v4/kcrypt/controllers"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("determineEnrollmentContext", func() {
	const (
		ns      = "default"
		tpmHash = "deadbeefdeadbeefdeadbeefdeadbeef"
	)

	var reconciler *controllers.SealedVolumeReconciler

	BeforeEach(func() {
		scheme := runtime.NewScheme()
		Expect(clientgoscheme.AddToScheme(scheme)).To(Succeed())
		Expect(keyserverv1alpha1.AddToScheme(scheme)).To(Succeed())
		reconciler = &controllers.SealedVolumeReconciler{
			Client: fake.NewClientBuilder().WithScheme(scheme).Build(),
			Scheme: scheme,
		}

		Expect(reconciler.Create(context.TODO(), &keyserverv1alpha1.SealedVolume{
			ObjectMeta: metav1.ObjectMeta{Name: "tofu-deadbeef", Namespace: ns},
			Spec: keyserverv1alpha1.SealedVolumeSpec{
				TPMHash: tpmHash,
				Partitions: []keyserverv1alpha1.PartitionSpec{
					{Label: "COS_PERSISTENT", DeviceName: "sda2", UUID: "uuid-persistent", Secret: &keyserverv1alpha1.SecretSpec{Name: "secret-persistent", Path: "pass"}},
				},
			},
		})).To(Succeed())
	})

	It("returns the error for a reinstalled partition and no passphrase data", func() {
		ctx, err := determineEnrollmentContext(reconciler, ns, tpmHash,
			PartitionInfo{Label: "COS_PERSISTENT", DeviceName: "sda2", UUID: "uuid-after-reinstall"}, logr.Discard())
		Expect(err).To(MatchError(ContainSubstring("tofu-deadbeef")))
		Expect(err).To(MatchError(ContainSubstring("COS_PERSISTENT")))
		Expect(ctx).To(BeNil())
	})

	It("returns the stored entry when the UUID matches", func() {
		ctx, err := determineEnrollmentContext(reconciler, ns, tpmHash,
			PartitionInfo{Label: "COS_PERSISTENT", DeviceName: "sda2", UUID: "uuid-persistent"}, logr.Discard())
		Expect(err).ToNot(HaveOccurred())
		Expect(ctx.IsNewPartition).To(BeFalse())
		Expect(ctx.VolumeData.SecretName).To(Equal("secret-persistent"))
	})

	It("treats a partition that only shares a device name as new", func() {
		ctx, err := determineEnrollmentContext(reconciler, ns, tpmHash,
			PartitionInfo{Label: "DATA", DeviceName: "sda2", UUID: "uuid-data"}, logr.Discard())
		Expect(err).ToNot(HaveOccurred())
		Expect(ctx.IsNewPartition).To(BeTrue())
		Expect(ctx.VolumeData.SecretName).To(BeEmpty())
	})
})
