package operator

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
)

var _ = Describe("uploadExporter", func() {
	It("emits a Job with the artifacts mount, upload env, and retries", func() {
		spec := uploadExporter("build-42")

		Expect(spec.Template.Spec.RestartPolicy).To(Equal(corev1.RestartPolicyNever))
		Expect(spec.BackoffLimit).NotTo(BeNil(),
			"BackoffLimit is nil; a transient network blip would fail the whole build")
		Expect(spec.Template.Spec.Containers).To(HaveLen(1))

		c := spec.Template.Spec.Containers[0]

		// The operator injects the "artifacts" Volume itself; exporters
		// must declare the read-only mount to see /artifacts.
		Expect(c.VolumeMounts).To(ContainElement(SatisfyAll(
			HaveField("Name", "artifacts"),
			HaveField("MountPath", "/artifacts"),
			HaveField("ReadOnly", true),
		)))

		Expect(c.Env).To(ContainElement(SatisfyAll(
			HaveField("Name", "BUILD_ID"),
			HaveField("Value", "build-42"),
		)))

		Expect(c.EnvFrom).To(HaveLen(1))
		Expect(c.EnvFrom[0].SecretRef).NotTo(BeNil())
		Expect(c.EnvFrom[0].SecretRef.Name).To(Equal(uploadSecretName("build-42")),
			"exporter reads AURORABOOT_URL and AURORABOOT_UPLOAD_TOKEN from this Secret")
	})
})
