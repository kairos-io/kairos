//go:build operator_e2e

package operator

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	clusterName        = "auroraboot-op-e2e"
	operatorNamespace  = "operator-system"
	operatorDeployment = "operator-kairos-operator"
	operatorKustomize  = "github.com/kairos-io/kairos-operator/config/default?ref=v0.1.1"
)

var (
	kubeconfigDir  string
	kubeconfigPath string
	keepCluster    bool
	clusterReused  bool
)

func TestOperator(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "AuroraBoot Operator E2E Suite")
}

var _ = BeforeSuite(func() {
	if _, err := exec.LookPath("kind"); err != nil {
		Skip("kind not on PATH, skipping operator e2e suite")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		Skip("docker not on PATH, skipping operator e2e suite")
	}
	if _, err := exec.LookPath("kubectl"); err != nil {
		Skip("kubectl not on PATH, skipping operator e2e suite")
	}
	if out, err := exec.Command("docker", "info").CombinedOutput(); err != nil {
		fmt.Fprintln(GinkgoWriter, string(out))
		Skip("docker daemon not reachable, skipping operator e2e suite")
	}

	// KEEP_CLUSTER is a bool-ish env var: accept anything strconv.ParseBool
	// takes (1/0, t/f, true/false, TRUE/FALSE, ...). Empty or unparseable
	// values leave the flag false, which means "tear the cluster down".
	if v, err := strconv.ParseBool(os.Getenv("KEEP_CLUSTER")); err == nil {
		keepCluster = v
	}

	var err error
	kubeconfigDir, err = os.MkdirTemp("", "auroraboot-op-e2e-")
	Expect(err).NotTo(HaveOccurred(), "create kubeconfig tempdir")
	kubeconfigPath = filepath.Join(kubeconfigDir, "kubeconfig")

	createCluster()
	Expect(os.Setenv("KUBECONFIG", kubeconfigPath)).To(Succeed())

	installOperator()
	testClient = buildTestClient()
})

var _ = AfterSuite(func() {
	if keepCluster {
		fmt.Fprintf(GinkgoWriter, "KEEP_CLUSTER set, leaving cluster %q and kubeconfig %s in place\n", clusterName, kubeconfigPath)
		return
	}
	// Only tear down clusters this suite created. If we reused an existing
	// one, deletion would clobber whatever the developer was iterating on.
	if clusterReused {
		fmt.Fprintf(GinkgoWriter, "cluster %q was reused, not deleting it\n", clusterName)
	} else if kubeconfigPath != "" {
		cmd := exec.Command("kind", "delete", "cluster", "--name", clusterName)
		cmd.Stdout = GinkgoWriter
		cmd.Stderr = GinkgoWriter
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(GinkgoWriter, "kind delete cluster failed: %v\n", err)
		}
	}
	if kubeconfigDir != "" {
		if err := os.RemoveAll(kubeconfigDir); err != nil {
			fmt.Fprintf(GinkgoWriter, "removing kubeconfig tempdir failed: %v\n", err)
		}
	}
})

func createCluster() {
	kindConfig, err := filepath.Abs("kind.yaml")
	Expect(err).NotTo(HaveOccurred(), "resolve kind.yaml path")

	args := []string{
		"create", "cluster",
		"--name", clusterName,
		"--config", kindConfig,
		"--kubeconfig", kubeconfigPath,
		"--wait", "90s",
	}
	cmd := exec.Command("kind", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()

	stderrStr := stderr.String()
	fmt.Fprint(GinkgoWriter, stdout.String())
	fmt.Fprint(GinkgoWriter, stderrStr)

	if err != nil {
		if strings.Contains(stderrStr, "already exist") {
			clusterReused = true
			fmt.Fprintf(GinkgoWriter, "kind cluster %q already exists, reusing\n", clusterName)
			exportKubeconfig()
			return
		}
		Fail(fmt.Sprintf("kind create cluster failed: %v", err))
	}
}

func exportKubeconfig() {
	cmd := exec.Command("kind", "export", "kubeconfig",
		"--name", clusterName,
		"--kubeconfig", kubeconfigPath,
	)
	out, err := cmd.CombinedOutput()
	fmt.Fprint(GinkgoWriter, string(out))
	Expect(err).NotTo(HaveOccurred(), "export kubeconfig for existing cluster")
}

func installOperator() {
	// v0.1.0 of kairos-operator does not require cert-manager: its
	// config/default/kustomization.yaml keeps cert-manager and webhook
	// entries commented out. We only apply the default overlay.
	apply := exec.Command("kubectl", "apply", "-k", operatorKustomize)
	apply.Env = append(os.Environ(), "KUBECONFIG="+kubeconfigPath)
	out, err := apply.CombinedOutput()
	fmt.Fprint(GinkgoWriter, string(out))
	Expect(err).NotTo(HaveOccurred(), "kubectl apply -k operator kustomization")

	Eventually(func() error {
		cmd := exec.Command("kubectl", "-n", operatorNamespace, "get", "deployment", operatorDeployment)
		cmd.Env = append(os.Environ(), "KUBECONFIG="+kubeconfigPath)
		return cmd.Run()
	}, 60*time.Second, 2*time.Second).Should(Succeed(), "operator deployment appears")

	wait := exec.Command("kubectl", "wait",
		"--for=condition=Available",
		"--timeout=180s",
		"-n", operatorNamespace,
		"deployment/"+operatorDeployment,
	)
	wait.Env = append(os.Environ(), "KUBECONFIG="+kubeconfigPath)
	waitOut, err := wait.CombinedOutput()
	fmt.Fprint(GinkgoWriter, string(waitOut))
	Expect(err).NotTo(HaveOccurred(), "operator deployment becomes Available")
}
