package provider

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func listenerOptionsForTest(apiAddress string, userEnv map[string]string) map[string]string {
	opts := map[string]string{"APILISTEN": normalizeAPIAddress(apiAddress)}
	applyAPIListenerEnv(opts, userEnv)
	return opts
}

var _ = Describe("EdgeVPN API listener options", func() {
	It("uses a secure Unix socket by default", func() {
		opts := listenerOptionsForTest("", nil)
		Expect(opts).To(HaveKeyWithValue("APILISTEN", DefaultEdgeVPNAPIAddress))
		Expect(opts).To(HaveKeyWithValue("APILISTENUNIXMODE", DefaultEdgeVPNAPISocketMode))
	})

	It("adds the secure mode when user configuration changes TCP to Unix", func() {
		opts := listenerOptionsForTest("http://localhost:8080", map[string]string{
			"APILISTEN": "unix:///run/custom-edgevpn.sock",
		})
		Expect(opts).To(HaveKeyWithValue("APILISTENUNIXMODE", DefaultEdgeVPNAPISocketMode))
	})

	It("does not add a Unix mode when user configuration changes Unix to TCP", func() {
		opts := listenerOptionsForTest(DefaultEdgeVPNAPIAddress, map[string]string{
			"APILISTEN": "127.0.0.1:8080",
		})
		Expect(opts).NotTo(HaveKey("APILISTENUNIXMODE"))
	})

	It("preserves an explicit Unix socket mode", func() {
		opts := listenerOptionsForTest("", map[string]string{"APILISTENUNIXMODE": "0660"})
		Expect(opts).To(HaveKeyWithValue("APILISTENUNIXMODE", "0660"))
	})
})

var _ = Describe("Resolving the API address the daemon actually listens on", func() {
	var envFile string

	writeEnv := func(contents string) {
		dir := GinkgoT().TempDir()
		envFile = filepath.Join(dir, "edgevpn-kairos.env")
		Expect(os.WriteFile(envFile, []byte(contents), 0600)).To(Succeed())
	}

	// SetupVPN and SetupAPI write APILISTEN into this file for the daemon.
	// Reading it back is what keeps the command line client pointed at the
	// same place, instead of both sides keeping their own default and
	// happening to agree.
	It("reads a unix socket listener back unchanged", func() {
		writeEnv("APILISTEN=\"unix:///run/edgevpn-kairos.sock\"\n")
		Expect(ResolveAPIAddress(envFile)).To(Equal("unix:///run/edgevpn-kairos.sock"))
	})

	// normalizeAPIAddress strips the scheme before writing APILISTEN, but the
	// API client builds request URLs by concatenating host and path, so a bare
	// host:port would produce "127.0.0.1:8080/api/..." and fail to parse.
	It("puts the scheme back on a TCP listener", func() {
		writeEnv("APILISTEN=\"127.0.0.1:8080\"\n")
		Expect(ResolveAPIAddress(envFile)).To(Equal("http://127.0.0.1:8080"))
	})

	It("falls back to the default when the file does not exist", func() {
		Expect(ResolveAPIAddress("/nonexistent/edgevpn-kairos.env")).To(Equal(DefaultEdgeVPNAPIAddress))
	})

	It("falls back to the default when the file has no APILISTEN", func() {
		writeEnv("EDGEVPNTOKEN=\"sometoken\"\n")
		Expect(ResolveAPIAddress(envFile)).To(Equal(DefaultEdgeVPNAPIAddress))
	})

	It("falls back to the default when APILISTEN is empty", func() {
		writeEnv("APILISTEN=\"\"\n")
		Expect(ResolveAPIAddress(envFile)).To(Equal(DefaultEdgeVPNAPIAddress))
	})
})

var _ = Describe("Writing the daemon's env file", func() {
	var rootDir string

	BeforeEach(func() {
		rootDir = GinkgoT().TempDir()
	})

	// The directory is created relative to rootDir, like the file in it, so an
	// image build writing into a staging root does not touch the host's /etc.
	It("creates the file under rootDir", func() {
		Expect(writeEdgeVPNEnv(rootDir, map[string]string{"APILISTEN": "127.0.0.1:8080"})).To(Succeed())
		Expect(filepath.Join(rootDir, EdgeVPNEnvFile)).To(BeAnExistingFile())
	})

	// A directory needs its execute bit to be entered at all. Created without
	// one, nothing can open the file inside it, including the edgevpn unit
	// that the file exists for.
	It("creates a directory it is possible to enter", func() {
		Expect(writeEdgeVPNEnv(rootDir, map[string]string{"APILISTEN": "127.0.0.1:8080"})).To(Succeed())

		info, err := os.Stat(filepath.Dir(filepath.Join(rootDir, EdgeVPNEnvFile)))
		Expect(err).NotTo(HaveOccurred())
		Expect(info.IsDir()).To(BeTrue())
		Expect(info.Mode().Perm()&0100).NotTo(BeZero(),
			"directory mode %v has no owner execute bit, so it cannot be traversed", info.Mode().Perm())
	})

	// Writing the daemon's configuration is not optional. If it fails the node
	// will not come up, so say so rather than carrying on.
	It("reports a failure instead of ignoring it", func() {
		blocked := filepath.Join(GinkgoT().TempDir(), "not-a-dir")
		Expect(os.WriteFile(blocked, []byte("i am a file"), 0600)).To(Succeed())

		Expect(writeEdgeVPNEnv(blocked, map[string]string{"APILISTEN": "127.0.0.1:8080"})).NotTo(Succeed())
	})

	// The reader and the writer agree, which is the point of sharing the path.
	It("writes something ResolveAPIAddress can read back", func() {
		Expect(writeEdgeVPNEnv(rootDir, map[string]string{"APILISTEN": "127.0.0.1:8080"})).To(Succeed())
		Expect(ResolveAPIAddress(filepath.Join(rootDir, EdgeVPNEnvFile))).To(Equal("http://127.0.0.1:8080"))
	})
})

var _ = Describe("Saving a generated cloud config", func() {
	var rootDir string

	BeforeEach(func() {
		rootDir = GinkgoT().TempDir()
	})

	It("writes the config under rootDir", func() {
		Expect(SaveCloudConfig(rootDir, "vpn_dns", []byte("#cloud-config\n"))).To(Succeed())

		written := filepath.Join(rootDir, CloudConfigDir, "vpn_dns.yaml")
		Expect(written).To(BeAnExistingFile())

		contents, err := os.ReadFile(written)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(contents)).To(Equal("#cloud-config\n"))
	})

	// Rotation runs on a node where the directory exists, and in a staging
	// root where it does not yet. Only one of those two gets tested by
	// accident, so create the directory rather than assume it.
	It("creates the directory when it is not there yet", func() {
		Expect(SaveCloudConfig(rootDir, "vpn_dns", []byte("#cloud-config\n"))).To(Succeed())

		info, err := os.Stat(filepath.Join(rootDir, CloudConfigDir))
		Expect(err).NotTo(HaveOccurred())
		Expect(info.IsDir()).To(BeTrue())
	})

	// The operator runs rotate-token from wherever they happen to be standing
	// and says which root to write into with a flag. A path that is not joined
	// to that root follows the working directory instead, which either fails
	// outright or quietly writes a config nothing will ever read.
	It("ignores the working directory", func() {
		elsewhere := GinkgoT().TempDir()
		GinkgoT().Chdir(elsewhere)

		Expect(SaveCloudConfig(rootDir, "vpn_dns", []byte("#cloud-config\n"))).To(Succeed())

		entries, err := os.ReadDir(elsewhere)
		Expect(err).NotTo(HaveOccurred())
		Expect(entries).To(BeEmpty())
	})

	// The rotation has already rewritten the token into the config files by
	// the time this runs, so a failure here leaves the node half rotated.
	// Saying so is the difference between a fixable state and a mystery.
	It("reports a failure instead of ignoring it", func() {
		blocked := filepath.Join(GinkgoT().TempDir(), "not-a-dir")
		Expect(os.WriteFile(blocked, []byte("i am a file"), 0600)).To(Succeed())

		Expect(SaveCloudConfig(blocked, "vpn_dns", []byte("#cloud-config\n"))).NotTo(Succeed())
	})
})
