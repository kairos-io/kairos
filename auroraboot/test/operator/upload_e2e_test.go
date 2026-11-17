//go:build operator_e2e

package operator

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	opbuilder "github.com/kairos-io/AuroraBoot/internal/builder/operator"
	"github.com/kairos-io/AuroraBoot/pkg/builder"
)

var _ = Describe("Operator exporter uploads artifacts back to AuroraBoot", func() {
	const (
		buildID     = "upload-e2e"
		token       = "test-upload-token-abcdef"
		sinkName    = "mock-upload-sink"
		sinkPort    = 8080
		sinkDataDir = "/data"
	)

	It("PUTs finished artifacts to the injected AuroraBoot upload endpoint", func() {
		ctx := context.Background()

		// Deploy a tiny in-cluster mock upload sink: a busybox httpd handling
		// PUTs by writing them into an emptyDir. Reachable from the exporter
		// Pod at http://mock-upload-sink.default.svc/api/v1/artifacts/...
		// which sidesteps the host-vs-cluster reachability question entirely.
		Expect(deployMockUploadSink(ctx, sinkName, testNamespace, sinkPort, sinkDataDir, token)).
			To(Succeed(), "deploy mock upload sink into the cluster")
		DeferCleanup(func() { _ = teardownMockUploadSink(ctx, sinkName, testNamespace) })

		auroraBootURL := fmt.Sprintf("http://%s.%s.svc:%d", sinkName, testNamespace, sinkPort)
		fmt.Fprintf(GinkgoWriter, "mock upload sink at %s\n", auroraBootURL)

		cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			clientcmd.NewDefaultClientConfigLoadingRules(),
			&clientcmd.ConfigOverrides{},
		).ClientConfig()
		Expect(err).NotTo(HaveOccurred(), "load kubeconfig")

		b, err := opbuilder.New(opbuilder.Config{
			RESTConfig:    cfg,
			Namespace:     testNamespace,
			AuroraBootURL: auroraBootURL,
		})
		Expect(err).NotTo(HaveOccurred(), "construct operator.Builder")

		// ISO output on purpose: the operator's CRD validator rejects an
		// artifacts block with no toggle enabled, so we cannot shortcut to
		// Stage-1-only. ISO is the cheapest full-pipeline output we can
		// trigger; its filename is deterministic (<id>.iso).
		_, err = b.Build(ctx, builder.BuildOptions{
			ID:          buildID,
			UploadToken: token,
			BaseImage:   "quay.io/kairos/opensuse:leap-15.6-core-amd64-generic-v3.6.0",
			Source:      builder.ImageSource{Arch: "amd64"},
			Outputs:     builder.OutputOptions{ISO: true},
		})
		Expect(err).NotTo(HaveOccurred(), "Build should submit the CR")

		DeferCleanup(cleanupArtifact, ctx, buildID)
		DeferCleanup(func() {
			if CurrentSpecReport().Failed() {
				collectDebugLogs(ctx, buildID)
				dumpExporterLogs(ctx, buildID)
				dumpSinkContents(ctx, sinkName, testNamespace, sinkDataDir)
			}
		})

		// 15 minutes covers a cold-cache CI run (pull operator tool image,
		// unpack rootfs, build ISO, run the exporter). Warm re-runs finish
		// in under 5 minutes.
		Eventually(func() ([]string, error) {
			return listSinkFiles(ctx, sinkName, testNamespace, sinkDataDir)
		}, 15*time.Minute, 10*time.Second).ShouldNot(BeEmpty(),
			"expected exporter Job to PUT at least one artifact to the in-cluster sink within 15 minutes")

		got, err := listSinkFiles(ctx, sinkName, testNamespace, sinkDataDir)
		Expect(err).NotTo(HaveOccurred())
		var sawISO bool
		for _, f := range got {
			if strings.HasSuffix(f, ".iso") {
				sawISO = true
				break
			}
		}
		Expect(sawISO).To(BeTrue(), "expected a .iso in uploads, got %v", got)
	})
})

// deployMockUploadSink stands up a Deployment + Service that accept PUTs on
// /api/v1/artifacts/<id>/upload/<file> and write bodies into an emptyDir.
// The server is a Python 3 http.server subclass; we tried a busybox nc loop
// first, but nc's unidirectional pipe pattern cannot write an HTTP response
// back to the same connection, and curl saw "Empty reply from server".
func deployMockUploadSink(ctx context.Context, name, ns string, port int, dataDir, token string) error {
	script := fmt.Sprintf(`
import http.server, os
DATA = %[2]q
TOK = %[3]q
os.makedirs(DATA, exist_ok=True)
class H(http.server.BaseHTTPRequestHandler):
    def do_PUT(self):
        if self.headers.get("Authorization", "") != "Bearer " + TOK:
            self.send_response(401); self.end_headers(); return
        base = os.path.basename(self.path)
        clen = int(self.headers.get("Content-Length", 0))
        with open(os.path.join(DATA, base), "wb") as f:
            remaining = clen
            while remaining > 0:
                chunk = self.rfile.read(min(65536, remaining))
                if not chunk:
                    break
                f.write(chunk)
                remaining -= len(chunk)
        self.send_response(201); self.end_headers()
    def log_message(self, fmt, *args):
        print("sink:", fmt %% args, flush=True)
http.server.ThreadingHTTPServer(("0.0.0.0", %[1]d), H).serve_forever()
`, port, dataDir, token)

	replicas := int32(1)
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": name}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": name}},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:    "sink",
							Image:   "python:3.13-alpine",
							Command: []string{"python3", "-c", script},
							Ports:   []corev1.ContainerPort{{ContainerPort: int32(port)}},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "data", MountPath: dataDir},
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromInt(port)},
								},
								InitialDelaySeconds: 2,
								PeriodSeconds:       2,
							},
						},
					},
					Volumes: []corev1.Volume{
						{Name: "data", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
					},
				},
			},
		},
	}
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": name},
			Ports:    []corev1.ServicePort{{Port: int32(port), TargetPort: intstr.FromInt(port)}},
		},
	}
	if err := testClient.Create(ctx, dep); err != nil {
		return fmt.Errorf("create deployment: %w", err)
	}
	if err := testClient.Create(ctx, svc); err != nil {
		return fmt.Errorf("create service: %w", err)
	}
	// Wait for the deployment to be Available so the Service has an endpoint
	// by the time the exporter tries to reach it.
	Eventually(func() (int32, error) {
		got := &appsv1.Deployment{}
		if err := testClient.Get(ctx, client.ObjectKey{Name: name, Namespace: ns}, got); err != nil {
			return 0, err
		}
		return got.Status.ReadyReplicas, nil
	}, 2*time.Minute, 3*time.Second).Should(BeNumerically(">=", 1), "mock upload sink pod becomes ready")
	return nil
}

func teardownMockUploadSink(ctx context.Context, name, ns string) error {
	dep := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}}
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}}
	_ = testClient.Delete(ctx, dep)
	_ = testClient.Delete(ctx, svc)
	return nil
}

// listSinkFiles returns the filenames the sink has recorded so far, by
// kubectl-exec'ing `ls` into the sink Pod. Filters common shell noise so a
// missing directory is treated as "nothing yet".
func listSinkFiles(_ context.Context, name, ns, dataDir string) ([]string, error) {
	out, err := exec.Command("kubectl", "-n", ns,
		"exec", "deploy/"+name, "--",
		"sh", "-c", "ls -1 "+dataDir+" 2>/dev/null || true").Output()
	if err != nil {
		return nil, fmt.Errorf("kubectl exec ls: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var files []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			files = append(files, l)
		}
	}
	return files, nil
}

func dumpSinkContents(_ context.Context, name, ns, dataDir string) {
	fmt.Fprintf(GinkgoWriter, "\n--- mock sink %s files ---\n", name)
	out, _ := exec.Command("kubectl", "-n", ns,
		"exec", "deploy/"+name, "--",
		"sh", "-c", "ls -la "+dataDir+" 2>/dev/null || true").CombinedOutput()
	fmt.Fprint(GinkgoWriter, string(out))
}

// dumpExporterLogs prints the exporter Job's Pod logs to GinkgoWriter.
// Exporter Jobs are named <artifact>-export-<index> per the operator and
// their Pods carry the standard job-name selector.
func dumpExporterLogs(_ context.Context, artifactName string) {
	jobName := artifactName + "-export-0"
	fmt.Fprintf(GinkgoWriter, "\n--- kubectl get jobs ---\n")
	jobs, _ := exec.Command("kubectl", "-n", testNamespace, "get", "jobs").CombinedOutput()
	fmt.Fprint(GinkgoWriter, string(jobs))
	fmt.Fprintf(GinkgoWriter, "\n--- kubectl get pods -l job-name=%s ---\n", jobName)
	pods, _ := exec.Command("kubectl", "-n", testNamespace, "get", "pods",
		"-l", "job-name="+jobName, "-o", "wide").CombinedOutput()
	fmt.Fprint(GinkgoWriter, string(pods))
	fmt.Fprintf(GinkgoWriter, "\n--- kubectl logs -l job-name=%s ---\n", jobName)
	logs, _ := exec.Command("kubectl", "-n", testNamespace, "logs",
		"-l", "job-name="+jobName, "--tail=200").CombinedOutput()
	fmt.Fprint(GinkgoWriter, string(logs))
}
