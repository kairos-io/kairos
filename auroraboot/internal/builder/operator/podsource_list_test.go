package operator

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

var _ = Describe("clientsetPodSource.List", func() {
	// The operator's CRD strips labels off spec.exporters[].template.metadata,
	// so exporter Pods carry only the Job controller's job-name label - not
	// build.kairos.io/artifact. List must reach them through the Job (which
	// IS labelled) so streamAllArtifactPods sees the upload output.
	const buildID = "b1"
	const ns = "kairos-builds"

	It("returns the builder Pod when only it exists", func() {
		cs := k8sfake.NewSimpleClientset(
			builderPodFixture(buildID, ns, "builder-1"),
		)
		src := newClientsetPodSource(cs, ns)

		got, err := src.List(context.Background(), buildID)
		Expect(err).NotTo(HaveOccurred())
		Expect(names(got)).To(ConsistOf("builder-1"))
	})

	It("finds the exporter Pod via its Job's build label + the job-name label", func() {
		cs := k8sfake.NewSimpleClientset(
			builderPodFixture(buildID, ns, "builder-1"),
			exporterJobFixture(buildID, ns, buildID+"-export-0"),
			exporterPodFixture(ns, buildID+"-export-0-xyz", buildID+"-export-0"),
		)
		src := newClientsetPodSource(cs, ns)

		got, err := src.List(context.Background(), buildID)
		Expect(err).NotTo(HaveOccurred())
		Expect(names(got)).To(ConsistOf("builder-1", buildID+"-export-0-xyz"))
	})

	It("returns all Pods for a Job that spawned retries", func() {
		cs := k8sfake.NewSimpleClientset(
			exporterJobFixture(buildID, ns, buildID+"-export-0"),
			exporterPodFixture(ns, buildID+"-export-0-try1", buildID+"-export-0"),
			exporterPodFixture(ns, buildID+"-export-0-try2", buildID+"-export-0"),
		)
		src := newClientsetPodSource(cs, ns)

		got, err := src.List(context.Background(), buildID)
		Expect(err).NotTo(HaveOccurred())
		Expect(names(got)).To(ConsistOf(buildID+"-export-0-try1", buildID+"-export-0-try2"))
	})

	It("ignores Jobs and Pods for other builds", func() {
		cs := k8sfake.NewSimpleClientset(
			builderPodFixture(buildID, ns, "builder-1"),
			builderPodFixture("other-build", ns, "builder-other"),
			exporterJobFixture(buildID, ns, buildID+"-export-0"),
			exporterJobFixture("other-build", ns, "other-build-export-0"),
			exporterPodFixture(ns, buildID+"-export-0-mine", buildID+"-export-0"),
			exporterPodFixture(ns, "other-build-export-0-theirs", "other-build-export-0"),
		)
		src := newClientsetPodSource(cs, ns)

		got, err := src.List(context.Background(), buildID)
		Expect(err).NotTo(HaveOccurred())
		Expect(names(got)).To(ConsistOf("builder-1", buildID+"-export-0-mine"))
	})
})

func builderPodFixture(buildID, ns, name string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels:    map[string]string{podBuildLabel: buildID},
		},
	}
}

func exporterJobFixture(buildID, ns, name string) *batchv1.Job {
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels:    map[string]string{podBuildLabel: buildID},
		},
	}
}

func exporterPodFixture(ns, name, jobName string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels: map[string]string{
				"job-name":       jobName,
				"controller-uid": "fake-uid-" + name,
			},
		},
	}
}

func names(pods []corev1.Pod) []string {
	out := make([]string, len(pods))
	for i, p := range pods {
		out[i] = p.Name
	}
	return out
}
