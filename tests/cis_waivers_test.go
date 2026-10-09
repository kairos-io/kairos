package mos_test

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
)

// waiverFile is the waiver list the CIS DIL run is given with
// --waiver-file. It is the only record of why a control is not enforced,
// so it has to be readable as well as loadable.
var waiverFile = filepath.Join("assets", "cis-dil-waivers.yaml")

type cisWaiver struct {
	ExpirationDate string `yaml:"expiration_date"`
	Run            bool   `yaml:"run"`
	Justification  string `yaml:"justification"`
}

// Checks the waiver file itself, with no machine involved. A duplicate key
// is silently collapsed by every YAML loader, including the one
// cinc-auditor uses, so a waiver that is listed twice looks maintained
// while only the last copy has any effect: revoke a control in the first
// copy and the later one keeps it waived, and the benchmark stays green
// over a real hardening regression. No linter sees this file (the yamllint
// job is scoped to .github/workflows/, and `#cloud-config` fixtures in this
// directory cannot satisfy its comment rule), so the check lives here.
var _ = Describe("cis waiver file", Label("cis"), func() {
	It("declares every control once, in order, with a reason", func() {
		raw, err := os.ReadFile(waiverFile)
		Expect(err).ToNot(HaveOccurred())

		// Decoding into a map is what rejects a duplicate key: yaml.v3
		// reports "already defined at line N" rather than keeping the
		// last one.
		waivers := map[string]cisWaiver{}
		Expect(yaml.Unmarshal(raw, &waivers)).To(Succeed(),
			"%s must declare each control exactly once", waiverFile)

		// The node gives the order the keys appear in, which the map
		// does not.
		var doc yaml.Node
		Expect(yaml.Unmarshal(raw, &doc)).To(Succeed())
		Expect(doc.Content).ToNot(BeEmpty())
		mapping := doc.Content[0]
		Expect(mapping.Kind).To(Equal(yaml.MappingNode))

		var ids []string
		for i := 0; i < len(mapping.Content); i += 2 {
			ids = append(ids, mapping.Content[i].Value)
		}
		Expect(ids).To(HaveLen(len(waivers)))

		for i := 1; i < len(ids); i++ {
			Expect(cisIDLess(ids[i-1], ids[i])).To(BeTrue(),
				"%s lists %s after %s; keep the file ordered by control id so a "+
					"re-added block is visible in review", waiverFile, ids[i], ids[i-1])
		}

		for id, w := range waivers {
			Expect(w.Run).To(BeFalse(),
				"%s: a waiver with run: true does not skip the control", id)
			Expect(w.Justification).ToNot(BeEmpty(),
				"%s: every waiver has to say why the control cannot apply", id)
			Expect(w.ExpirationDate).ToNot(BeEmpty(),
				"%s: cinc-auditor ignores a waiver with no expiration_date", id)
		}
	})
})
