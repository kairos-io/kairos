//go:build operator_e2e

package operator

import (
	"context"
	"fmt"
	"os/exec"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	buildv1alpha2 "github.com/kairos-io/kairos-operator/api/v1alpha2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const testNamespace = "default"

// testClient is populated once by BeforeSuite via buildTestClient(). All
// helper functions reuse it; each spec pays for a single client construction
// per suite, not per helper call.
var testClient client.Client

// buildTestClient builds a controller-runtime client against KUBECONFIG with
// the kairos-operator v1alpha2 scheme registered. Called exactly once from
// BeforeSuite.
func buildTestClient() client.Client {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		loadingRules, &clientcmd.ConfigOverrides{},
	).ClientConfig()
	Expect(err).NotTo(HaveOccurred(), "load REST config from KUBECONFIG")

	scheme := runtime.NewScheme()
	Expect(clientgoscheme.AddToScheme(scheme)).To(Succeed(), "register client-go scheme")
	Expect(buildv1alpha2.AddToScheme(scheme)).To(Succeed(), "register v1alpha2 scheme")

	c, err := client.New(cfg, client.Options{Scheme: scheme})
	Expect(err).NotTo(HaveOccurred(), "construct controller-runtime client")
	return c
}

// createOSArtifact applies the given spec as a new OSArtifact in the test
// namespace and returns the persisted CR.
func createOSArtifact(ctx context.Context, name string, spec buildv1alpha2.OSArtifactSpec) *buildv1alpha2.OSArtifact {
	art := &buildv1alpha2.OSArtifact{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: spec,
	}
	Expect(testClient.Create(ctx, art)).To(Succeed(), "create OSArtifact %s", name)
	return art
}

// waitForPhase polls the OSArtifact's .status.phase every 2s until it matches
// the requested phase or the timeout expires.
func waitForPhase(ctx context.Context, name string, phase buildv1alpha2.ArtifactPhase, timeout time.Duration) {
	key := types.NamespacedName{Name: name, Namespace: testNamespace}

	Eventually(func() (buildv1alpha2.ArtifactPhase, error) {
		got := &buildv1alpha2.OSArtifact{}
		if err := testClient.Get(ctx, key, got); err != nil {
			return "", err
		}
		return got.Status.Phase, nil
	}, timeout, 2*time.Second).Should(Equal(phase), "OSArtifact %s reaches phase %s", name, phase)
}

// cleanupArtifact deletes the OSArtifact and tolerates NotFound. Owned Pods
// are garbage-collected by the operator via owner references.
func cleanupArtifact(ctx context.Context, name string) {
	art := &buildv1alpha2.OSArtifact{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
	}
	if err := testClient.Delete(ctx, art); err != nil && !apierrors.IsNotFound(err) {
		fmt.Fprintf(GinkgoWriter, "cleanupArtifact: delete %s failed: %v\n", name, err)
	}
}

// collectDebugLogs dumps `kubectl describe osartifact <name>` and the tailed
// builder-pod logs to GinkgoWriter. Called from a DeferCleanup on failure.
func collectDebugLogs(_ context.Context, artifactName string) {
	fmt.Fprintf(GinkgoWriter, "\n--- kubectl describe osartifact/%s ---\n", artifactName)
	describe := exec.Command("kubectl", "-n", testNamespace, "describe", "osartifact", artifactName)
	out, _ := describe.CombinedOutput()
	fmt.Fprint(GinkgoWriter, string(out))

	fmt.Fprintf(GinkgoWriter, "\n--- kubectl logs -l build.kairos.io/artifact=%s ---\n", artifactName)
	logs := exec.Command("kubectl", "-n", testNamespace,
		"logs", "-l", "build.kairos.io/artifact="+artifactName,
		"--tail=200", "--all-containers=true",
	)
	logsOut, _ := logs.CombinedOutput()
	fmt.Fprint(GinkgoWriter, string(logsOut))
}

// minimalSpec returns the smallest OSArtifactSpec that lets the operator
// progress past validation into the Building phase: a pre-built Kairos image
// (no from-scratch build), amd64, ISO output, no exporters.
func minimalSpec() buildv1alpha2.OSArtifactSpec {
	return buildv1alpha2.OSArtifactSpec{
		Image: buildv1alpha2.ImageSpec{
			Ref: "quay.io/kairos/opensuse:leap-15.6-core-amd64-generic-v3.6.0",
		},
		Artifacts: &buildv1alpha2.ArtifactSpec{
			Arch: "amd64",
			ISO:  true,
		},
	}
}
