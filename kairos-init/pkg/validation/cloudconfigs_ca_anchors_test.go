package validation_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
)

type caAnchorStage struct {
	Name     string   `yaml:"name"`
	If       string   `yaml:"if"`
	Commands []string `yaml:"commands"`
}

type caAnchorConfig struct {
	Stages map[string][]caAnchorStage `yaml:"stages"`
}

func readCAAnchorStages() []caAnchorStage {
	content, err := os.ReadFile(filepath.Join("..", "bundled", "cloudconfigs", "01_ca_anchors.yaml"))
	Expect(err).NotTo(HaveOccurred(), "read 01_ca_anchors.yaml")

	var cfg caAnchorConfig
	Expect(yaml.Unmarshal(content, &cfg)).To(Succeed(), "parse 01_ca_anchors.yaml")
	return cfg.Stages["initramfs"]
}

// caAnchorRootfs writes a throwaway rootfs. tools are executables to create,
// relative to the root, and anchors are anchor directories to create. An
// anchor path with a trailing "/cert.pem" gets a file in it, so that a spec can
// tell an empty anchor directory from one an operator has used.
func caAnchorRootfs(tools, dirs, files []string) string {
	root := GinkgoT().TempDir()
	for _, tool := range tools {
		path := filepath.Join(root, tool)
		Expect(os.MkdirAll(filepath.Dir(path), 0o755)).To(Succeed())
		Expect(os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755)).To(Succeed())
	}
	for _, dir := range dirs {
		Expect(os.MkdirAll(filepath.Join(root, dir), 0o755)).To(Succeed())
	}
	for _, file := range files {
		path := filepath.Join(root, file)
		Expect(os.MkdirAll(filepath.Dir(path), 0o755)).To(Succeed())
		Expect(os.WriteFile(path, []byte("-----BEGIN CERTIFICATE-----\n"), 0o644)).To(Succeed())
	}
	return root
}

// caAnchorGuard runs a stage's if expression the way yip does, with sh -c,
// against root. Every absolute /usr and /etc path in the expression is
// rewritten to sit under root.
func caAnchorGuard(expr, root string) bool {
	expr = strings.ReplaceAll(expr, " /usr/", " "+root+"/usr/")
	expr = strings.ReplaceAll(expr, " /etc/", " "+root+"/etc/")
	return exec.Command("sh", "-c", expr).Run() == nil
}

const (
	redHatTool    = "usr/bin/update-ca-trust"
	suseTool      = "usr/sbin/update-ca-certificates"
	redHatAnchors = "etc/pki/ca-trust/source/anchors"
	suseAnchors   = "etc/pki/trust/anchors"
)

var _ = Describe("Bundled cloudconfigs CA anchor re-extraction", func() {
	var redHat, suse caAnchorStage

	BeforeEach(func() {
		stages := readCAAnchorStages()
		Expect(stages).To(HaveLen(2), "one stage per family that needs a re-extract")
		redHat, suse = stages[0], stages[1]
	})

	// Pinned rather than asserted absent: a guard that never fires and a
	// command list that is empty both pass an absence-only check.
	It("runs each family's own extract tool", func() {
		Expect(redHat.Commands).To(Equal([]string{"update-ca-trust extract"}))
		Expect(suse.Commands).To(Equal([]string{"update-ca-certificates"}))
	})

	DescribeTable("the Red Hat guard",
		func(tools, dirs, files []string, expected bool) {
			Expect(caAnchorGuard(redHat.If, caAnchorRootfs(tools, dirs, files))).To(Equal(expected))
		},
		Entry("fires when the operator added an anchor",
			[]string{redHatTool}, nil, []string{redHatAnchors + "/corp.crt"}, true),
		Entry("stays quiet when the anchor directory is empty",
			[]string{redHatTool}, []string{redHatAnchors}, nil, false),
		Entry("stays quiet when the anchor directory is absent",
			[]string{redHatTool}, nil, nil, false),
		Entry("stays quiet when the image ships no update-ca-trust",
			nil, nil, []string{redHatAnchors + "/corp.crt"}, false),
	)

	DescribeTable("the SUSE guard",
		func(tools, dirs, files []string, expected bool) {
			Expect(caAnchorGuard(suse.If, caAnchorRootfs(tools, dirs, files))).To(Equal(expected))
		},
		Entry("fires when the operator added an anchor",
			[]string{suseTool}, nil, []string{suseAnchors + "/corp.crt"}, true),
		Entry("stays quiet when the anchor directory is empty",
			[]string{suseTool}, []string{suseAnchors}, nil, false),
		Entry("stays quiet when the anchor directory is absent",
			[]string{suseTool}, nil, nil, false),
		Entry("stays quiet when the image ships no update-ca-certificates",
			nil, nil, []string{suseAnchors + "/corp.crt"}, false),
	)

	// The two guards share a stage list, so each one has to stay off the other
	// family's machine even when that machine has an anchor to extract.
	It("keeps the two families apart", func() {
		rocky := caAnchorRootfs([]string{redHatTool}, nil, []string{redHatAnchors + "/corp.crt"})
		Expect(caAnchorGuard(redHat.If, rocky)).To(BeTrue())
		Expect(caAnchorGuard(suse.If, rocky)).To(BeFalse())

		leap := caAnchorRootfs([]string{suseTool}, nil, []string{suseAnchors + "/corp.crt"})
		Expect(caAnchorGuard(redHat.If, leap)).To(BeFalse())
		Expect(caAnchorGuard(suse.If, leap)).To(BeTrue())
	})

	// Debian, Ubuntu and Hadron take their drop-in from
	// /usr/local/share/ca-certificates and write into a real, persisted
	// /etc/ssl/certs, so neither stage has anything to do there. They do ship
	// update-ca-certificates, which is why the SUSE guard also tests for the
	// anchor directory and not only for the tool.
	It("does nothing on a Debian-shaped or Hadron-shaped rootfs", func() {
		debian := caAnchorRootfs(
			[]string{suseTool},
			[]string{"usr/local/share/ca-certificates", "etc/ssl/certs"},
			[]string{"usr/local/share/ca-certificates/corp.crt"},
		)
		Expect(caAnchorGuard(redHat.If, debian)).To(BeFalse())
		Expect(caAnchorGuard(suse.If, debian)).To(BeFalse())
	})
})
