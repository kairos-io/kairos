package challenger

import (
	keyserverv1alpha1 "github.com/kairos-io/kairos/v4/kcrypt/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("findPartitionInVolume", func() {
	var volume *keyserverv1alpha1.SealedVolume

	BeforeEach(func() {
		volume = &keyserverv1alpha1.SealedVolume{
			ObjectMeta: metav1.ObjectMeta{Name: "tofu-a1b2c3d4"},
			Spec: keyserverv1alpha1.SealedVolumeSpec{
				Partitions: []keyserverv1alpha1.PartitionSpec{
					{Label: "COS_OEM", DeviceName: "sda2", UUID: "uuid-oem", Secret: &keyserverv1alpha1.SecretSpec{Name: "secret-oem", Path: "pass"}},
					{Label: "DATA", DeviceName: "sdb2", UUID: "uuid-data", Secret: &keyserverv1alpha1.SecretSpec{Name: "secret-data", Path: "pass"}},
				},
			},
		}
	})

	find := func(p PartitionInfo) *SealedVolumeData {
		data, ok := findPartitionInVolume(volume, p)
		Expect(ok).To(BeTrue())
		return data
	}

	It("picks the entry whose label and UUID match over an earlier entry with the same device name", func() {
		// The kernel name of the DATA partition changed to the one stored for COS_OEM.
		data := find(PartitionInfo{Label: "DATA", DeviceName: "sda2", UUID: "uuid-data"})
		Expect(data.SecretName).To(Equal("secret-data"))
		Expect(data.PartitionLabel).To(Equal("DATA"))
	})

	It("prefers a UUID match over a label match", func() {
		data := find(PartitionInfo{Label: "COS_OEM", UUID: "uuid-data"})
		Expect(data.SecretName).To(Equal("secret-data"))
	})

	It("prefers a label match over a device name match", func() {
		data := find(PartitionInfo{Label: "DATA", DeviceName: "sda2"})
		Expect(data.SecretName).To(Equal("secret-data"))
	})

	It("matches on the device name alone", func() {
		data := find(PartitionInfo{DeviceName: "sdb2"})
		Expect(data.SecretName).To(Equal("secret-data"))
		Expect(data.VolumeName).To(Equal("tofu-a1b2c3d4"))
	})

	It("does not match on an unknown UUID", func() {
		volume.Spec.Partitions = []keyserverv1alpha1.PartitionSpec{
			{Label: "COS_PERSISTENT", Secret: &keyserverv1alpha1.SecretSpec{Name: "secret-persistent"}},
			{Label: "COS_OEM", UUID: "unknown", Secret: &keyserverv1alpha1.SecretSpec{Name: "secret-oem"}},
		}
		data := find(PartitionInfo{Label: "COS_PERSISTENT", UUID: "unknown"})
		Expect(data.SecretName).To(Equal("secret-persistent"))
	})

	It("does not match on empty fields", func() {
		volume.Spec.Partitions = []keyserverv1alpha1.PartitionSpec{
			{Label: "COS_OEM", Secret: &keyserverv1alpha1.SecretSpec{Name: "secret-oem"}},
		}
		_, ok := findPartitionInVolume(volume, PartitionInfo{Label: "DATA"})
		Expect(ok).To(BeFalse())
		_, ok = findPartitionInVolume(volume, PartitionInfo{})
		Expect(ok).To(BeFalse())
	})
})
