package challenger

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"

	"github.com/go-logr/logr"
	"github.com/google/go-attestation/attest"
	keyserverv1alpha1 "github.com/kairos-io/kairos/v4/kcrypt/api/v1alpha1"
	"github.com/kairos-io/kairos/v4/kcrypt/controllers"
	"github.com/kairos-io/kairos/v4/kcrypt/pkg/kube"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("TOFU object names", func() {
	const (
		ns    = "default"
		label = "COS_PERSISTENT"
	)
	// Two different TPMs whose hashes share the first 8 hex characters.
	hashA := "deadbeef" + "aaaaaaaaaaaaaaaaaaaaaaaa" + "00000000000000000000000000000000"
	hashB := "deadbeef" + "bbbbbbbbbbbbbbbbbbbbbbbb" + "00000000000000000000000000000000"

	var (
		kclient    *k8sfake.Clientset
		reconciler *controllers.SealedVolumeReconciler
		attest_    *ClientAttestation
	)

	enroll := func(hash string) (*EnrollmentContext, error) {
		ctx := &EnrollmentContext{TPMHash: hash, Partition: PartitionInfo{Label: label}}
		return ctx, performInitialEnrollment(ctx, attest_, reconciler, kclient, ns, logr.Discard())
	}

	passphraseOf := func(ctx *EnrollmentContext) string {
		s, err := kclient.CoreV1().Secrets(ns).Get(context.TODO(), ctx.VolumeData.SecretName, metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())
		return string(s.Data[ctx.VolumeData.SecretPath])
	}

	BeforeEach(func() {
		scheme := runtime.NewScheme()
		Expect(clientgoscheme.AddToScheme(scheme)).To(Succeed())
		Expect(keyserverv1alpha1.AddToScheme(scheme)).To(Succeed())
		reconciler = &controllers.SealedVolumeReconciler{
			Client: fake.NewClientBuilder().WithScheme(scheme).Build(),
			Scheme: scheme,
		}
		kclient = k8sfake.NewSimpleClientset()

		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		Expect(err).ToNot(HaveOccurred())
		attest_ = &ClientAttestation{
			EK:        &attest.EK{Public: &key.PublicKey},
			PCRValues: &keyserverv1alpha1.PCRValues{},
		}
	})

	It("enrolls two TPMs that share the first 8 hash characters with their own objects", func() {
		a, err := enroll(hashA)
		Expect(err).ToNot(HaveOccurred())
		b, err := enroll(hashB)
		Expect(err).ToNot(HaveOccurred())

		Expect(b.VolumeData.VolumeName).ToNot(Equal(a.VolumeData.VolumeName))
		Expect(b.VolumeData.SecretName).ToNot(Equal(a.VolumeData.SecretName))
		Expect(passphraseOf(b)).ToNot(Equal(passphraseOf(a)))
	})

	It("never hands one TPM the Secret of another", func() {
		a, err := enroll(hashA)
		Expect(err).ToNot(HaveOccurred())

		// B asks for the name A's Secret already has.
		_, err = createOrReuseTOFUSecret(kclient, ns, a.VolumeData.SecretName, "passphrase", "other", hashB, label, logr.Discard())
		Expect(err).To(MatchError(ContainSubstring("different TPM")))
	})

	It("reuses the Secret of the same TPM after its SealedVolume was deleted", func() {
		a, err := enroll(hashA)
		Expect(err).ToNot(HaveOccurred())
		first := passphraseOf(a)

		sv := &keyserverv1alpha1.SealedVolume{ObjectMeta: metav1.ObjectMeta{Name: a.VolumeData.VolumeName, Namespace: ns}}
		Expect(reconciler.Delete(context.TODO(), sv)).To(Succeed())

		again, err := enroll(hashA)
		Expect(err).ToNot(HaveOccurred())
		Expect(passphraseOf(again)).To(Equal(first))
	})

	It("keeps using the Secret an old 8-character enrollment of the same TPM left behind", func() {
		legacyName := kube.SafeKubeName("tofu-deadbeef-" + "cos-persistent")
		_, err := kclient.CoreV1().Secrets(ns).Create(context.TODO(), &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name: legacyName, Namespace: ns,
				Labels: map[string]string{tpmHashLabel: kube.SafeKubeName(hashA)},
			},
			Data: map[string][]byte{"passphrase": []byte("old-disk-passphrase")},
		}, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		a, err := enroll(hashA)
		Expect(err).ToNot(HaveOccurred())
		Expect(a.VolumeData.SecretName).To(Equal(legacyName))
		Expect(passphraseOf(a)).To(Equal("old-disk-passphrase"))
	})

	It("ignores an old 8-character Secret that belongs to another TPM", func() {
		legacyName := kube.SafeKubeName("tofu-deadbeef-" + "cos-persistent")
		_, err := kclient.CoreV1().Secrets(ns).Create(context.TODO(), &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name: legacyName, Namespace: ns,
				Labels: map[string]string{tpmHashLabel: kube.SafeKubeName(hashA)},
			},
			Data: map[string][]byte{"passphrase": []byte("old-disk-passphrase")},
		}, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		b, err := enroll(hashB)
		Expect(err).ToNot(HaveOccurred())
		Expect(b.VolumeData.SecretName).ToNot(Equal(legacyName))
		Expect(passphraseOf(b)).ToNot(Equal("old-disk-passphrase"))
	})
})
