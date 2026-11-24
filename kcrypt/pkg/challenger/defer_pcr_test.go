package challenger

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"

	"github.com/go-logr/logr"
	keyserverv1alpha1 "github.com/kairos-io/kairos/v4/kcrypt/api/v1alpha1"
	"github.com/kairos-io/kairos/v4/kcrypt/controllers"
	"github.com/kairos-io/kairos/v4/kcrypt/pkg/attestation"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("DeferPCREnrollment with a static passphrase", func() {
	const (
		ns         = "default"
		label      = "COS_PERSISTENT"
		volumeName = "static-volume"
		secretName = "static-passphrase"
		tpmHash    = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	)

	var (
		attestator *Attestator
		reconciler *controllers.SealedVolumeReconciler
		ekPEM      []byte
	)

	issue := func(deferPCRs bool, pcr11 byte) ([]byte, error) {
		return attestator.IssuePassphrase(context.TODO(), attestation.Request{
			TPMHash:            tpmHash,
			PCRs:               map[int][]byte{11: {pcr11, pcr11}},
			EKPEM:              ekPEM,
			DeferPCREnrollment: deferPCRs,
		})
	}

	storedPCR11 := func() string {
		sv := &keyserverv1alpha1.SealedVolume{}
		Expect(reconciler.Get(context.TODO(), types.NamespacedName{Namespace: ns, Name: volumeName}, sv)).To(Succeed())
		Expect(sv.Spec.Attestation).ToNot(BeNil())
		Expect(sv.Spec.Attestation.PCRValues).ToNot(BeNil())
		pcr, ok := sv.Spec.Attestation.PCRValues.PCRs["11"]
		Expect(ok).To(BeTrue())
		return pcr
	}

	BeforeEach(func() {
		scheme := runtime.NewScheme()
		Expect(clientgoscheme.AddToScheme(scheme)).To(Succeed())
		Expect(keyserverv1alpha1.AddToScheme(scheme)).To(Succeed())

		sv := &keyserverv1alpha1.SealedVolume{
			ObjectMeta: metav1.ObjectMeta{Name: volumeName, Namespace: ns},
			Spec: keyserverv1alpha1.SealedVolumeSpec{
				TPMHash: tpmHash,
				Partitions: []keyserverv1alpha1.PartitionSpec{{
					Label:  label,
					Secret: &keyserverv1alpha1.SecretSpec{Name: secretName, Path: "passphrase"},
				}},
			},
		}
		reconciler = &controllers.SealedVolumeReconciler{
			Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(sv).Build(),
			Scheme: scheme,
		}
		kclient := k8sfake.NewSimpleClientset(&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: ns},
			Data:       map[string][]byte{"passphrase": []byte("static")},
		})
		attestator = NewAttestator(reconciler, kclient, ns, logr.Discard(), PartitionInfo{Label: label})

		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		Expect(err).ToNot(HaveOccurred())
		der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
		Expect(err).ToNot(HaveOccurred())
		ekPEM = pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
	})

	It("does not store the live boot PCRs and enrolls the installed system's PCRs on first boot", func() {
		pass, err := issue(true, 0xaa)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(pass)).To(Equal("static"))
		Expect(storedPCR11()).To(BeEmpty())

		pass, err = issue(false, 0xbb)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(pass)).To(Equal("static"))
		Expect(storedPCR11()).To(Equal("bbbb"))

		_, err = issue(false, 0xcc)
		Expect(err).To(MatchError(ContainSubstring("PCR11 changed")))
	})
})
