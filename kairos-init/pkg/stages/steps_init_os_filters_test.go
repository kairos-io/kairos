package stages_test

import (
	"regexp"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/kairos/v4/kairos-init/pkg/stages"
	"github.com/kairos-io/kairos/v4/kairos-init/pkg/values"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
	"github.com/mudler/yip/pkg/schema"
)

// The OS filters are matched against os-release's PRETTY_NAME, not its ID or
// NAME, so these are the strings the supported distributions really report.
const (
	slesPrettyName       = "SUSE Linux Enterprise Server 15 SP6"
	leapPrettyName       = "openSUSE Leap 15.6"
	microPrettyName      = "SUSE Linux Enterprise Micro for Rancher 5.4"
	ubuntuPrettyName     = "Ubuntu 24.04.2 LTS"
	rockyPrettyName      = "Rocky Linux 9.8 (Blue Onyx)"
	hadronPrettyName     = "Hadron Linux"
	tumbleweedPrettyName = "openSUSE Tumbleweed"
)

// stageByName returns the single services stage with the given name.
func stageByName(result []schema.Stage, name string) schema.Stage {
	GinkgoHelper()
	var found []schema.Stage
	for _, s := range result {
		if s.Name == name {
			found = append(found, s)
		}
	}
	Expect(found).To(HaveLen(1), "expected exactly one stage named %q", name)
	return found[0]
}

// selects reports whether an OnlyIfOs filter picks the given PRETTY_NAME.
func selects(filter, prettyName string) bool {
	GinkgoHelper()
	re, err := regexp.Compile(filter)
	Expect(err).ToNot(HaveOccurred(), "OnlyIfOs %q does not compile", filter)
	return re.MatchString(prettyName)
}

var _ = Describe("GetServicesStage OS filters", func() {
	var result []schema.Stage

	BeforeEach(func() {
		result = stages.GetServicesStage(values.System{}, logger.NewKairosLogger("test", "error", true))
	})

	It("compiles every filter", func() {
		Expect(result).ToNot(BeEmpty())
		for _, s := range result {
			if s.OnlyIfOs == "" {
				continue
			}
			_, err := regexp.Compile(s.OnlyIfOs)
			Expect(err).ToNot(HaveOccurred(), "stage %q", s.Name)
		}
	})

	// A filter written as "SLES.*" reads as if it covers SLES and matches
	// nothing at all, because SLES reports PRETTY_NAME="SUSE Linux Enterprise
	// Server ...". Any filter that means to name SLES has to select it.
	It("selects SLES from every filter that claims to cover it", func() {
		for _, s := range result {
			if !strings.Contains(s.OnlyIfOs, "SLES") {
				continue
			}
			Expect(selects(s.OnlyIfOs, slesPrettyName)).To(BeTrue(),
				"stage %q lists SLES but its filter does not match %q", s.Name, slesPrettyName)
		}
	})

	DescribeTable("selects the distributions it is meant to",
		func(stageName string, prettyName string, want bool) {
			Expect(selects(stageByName(result, stageName).OnlyIfOs, prettyName)).To(Equal(want))
		},
		Entry("fail2ban on SLES", "Enable fail2ban service", slesPrettyName, true),
		Entry("fail2ban on openSUSE Leap", "Enable fail2ban service", leapPrettyName, true),
		Entry("fail2ban on Ubuntu", "Enable fail2ban service", ubuntuPrettyName, true),
		Entry("fail2ban not on SLE Micro", "Enable fail2ban service", microPrettyName, false),
		Entry("fail2ban not on Rocky", "Enable fail2ban service", rockyPrettyName, false),

		Entry("timesyncd on SLES", "Enable timesyncd service", slesPrettyName, true),
		Entry("timesyncd on openSUSE Leap", "Enable timesyncd service", leapPrettyName, true),
		Entry("timesyncd on openSUSE Tumbleweed", "Enable timesyncd service", tumbleweedPrettyName, true),
		Entry("timesyncd on Hadron", "Enable timesyncd service", hadronPrettyName, true),
		Entry("timesyncd not on SLE Micro", "Enable timesyncd service", microPrettyName, false),
		Entry("timesyncd not on Rocky", "Enable timesyncd service", rockyPrettyName, false),

		Entry("chronyd on Rocky", "Enable chronyd service for RHEL family and Fedora", rockyPrettyName, true),
		Entry("chronyd not on SLES", "Enable chronyd service for RHEL family and Fedora", slesPrettyName, false),
	)
})
