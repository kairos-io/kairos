package role

import (
	"os"
	"path/filepath"
	"strconv"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/tools/clientcmd"
)

// adminKubeconfig is the shape "k0s kubeconfig admin" prints: a flattened
// kubeconfig, credentials embedded rather than referenced by path.
const adminKubeconfig = `apiVersion: v1
kind: Config
clusters:
- cluster:
    certificate-authority-data: Zm9v
    server: https://10.1.0.3:6443
  name: local
contexts:
- context:
    cluster: local
    user: user
  name: Default
current-context: Default
users:
- name: user
  user:
    client-certificate-data: YmFy
    client-key-data: YmF6
`

// k0sStub writes an executable that records its arguments, prints stdout on
// stdout and stderr on stderr, and exits with code. It stands in for the k0s
// binary, which a test host does not have. The second return is the path the
// arguments are recorded in.
func k0sStub(stdout, stderr string, code int) (string, string) {
	dir := GinkgoT().TempDir()
	outFile := filepath.Join(dir, "stdout")
	errFile := filepath.Join(dir, "stderr")
	argvFile := filepath.Join(dir, "argv")
	Expect(os.WriteFile(outFile, []byte(stdout), 0600)).To(Succeed())
	Expect(os.WriteFile(errFile, []byte(stderr), 0600)).To(Succeed())

	script := "#!/bin/sh\n" +
		"echo \"$@\" > " + argvFile + "\n" +
		"cat " + outFile + "\n" +
		"cat " + errFile + " >&2\n" +
		"exit " + strconv.Itoa(code) + "\n"

	path := filepath.Join(dir, "k0s")
	Expect(os.WriteFile(path, []byte(script), 0700)).To(Succeed())

	return path, argvFile
}

var _ = Describe("K0sAdminKubeconfig", func() {
	It("asks k0s for the admin kubeconfig, not for the cluster config", func() {
		bin, argv := k0sStub(adminKubeconfig, "", 0)

		_, err := K0sAdminKubeconfig(bin)
		Expect(err).NotTo(HaveOccurred())

		// "config create" prints the default ClusterConfig, which carries no
		// credentials and is not a kubeconfig.
		Expect(os.ReadFile(argv)).To(BeEquivalentTo("kubeconfig admin\n"))
	})

	It("returns a kubeconfig kubectl can load", func() {
		bin, _ := k0sStub(adminKubeconfig, "", 0)

		out, err := K0sAdminKubeconfig(bin)
		Expect(err).NotTo(HaveOccurred())

		cfg, err := clientcmd.Load(out)
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.CurrentContext).To(Equal("Default"))
		Expect(cfg.Clusters["local"].Server).To(Equal("https://10.1.0.3:6443"))
		Expect(cfg.AuthInfos["user"].ClientCertificateData).NotTo(BeEmpty())
	})

	It("keeps what k0s logs to stderr out of the kubeconfig", func() {
		// The shape logrus writes when iface.FirstPublicAddress cannot read an
		// interface. k0s leaves logrus on its default sink, which is stderr.
		warning := `time="2026-10-03T09:12:01Z" level=warning msg="failed to find any non-local, non podnetwork addresses on host, defaulting public address to 127.0.0.1"` + "\n"
		bin, _ := k0sStub(adminKubeconfig, warning, 0)

		out, err := K0sAdminKubeconfig(bin)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(out)).To(Equal(adminKubeconfig))
		Expect(string(out)).NotTo(ContainSubstring("level=warning"))

		_, err = clientcmd.Load(out)
		Expect(err).NotTo(HaveOccurred())
	})

	It("reports what k0s said when the control plane is not up", func() {
		msg := "Error: admin PKI file \"/var/lib/k0s/pki/admin.crt\" not found, check if the control plane is initialized on this node\n"
		bin, _ := k0sStub("", msg, 1)

		out, err := K0sAdminKubeconfig(bin)
		Expect(err).To(HaveOccurred())
		Expect(out).To(BeNil())
		Expect(err.Error()).To(ContainSubstring("kubeconfig admin"))
		Expect(err.Error()).To(ContainSubstring("check if the control plane is initialized"))
	})

	It("reports a missing k0s binary rather than publishing nothing", func() {
		_, err := K0sAdminKubeconfig(filepath.Join(GinkgoT().TempDir(), "absent"))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("kubeconfig admin"))
	})
})
