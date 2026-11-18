package hardware_test

import (
	"context"
	"errors"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/AuroraBoot/pkg/hardware"
	"github.com/kairos-io/AuroraBoot/pkg/redfish"
)

func TestHardware(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Hardware Suite")
}

// fakeInspector implements the systemInspector contract the Inspector depends
// on, returning canned SystemInfo (or an error) without touching a real BMC.
type fakeInspector struct {
	info *redfish.SystemInfo
	err  error
}

func (f fakeInspector) Inspect(_ context.Context) (*redfish.SystemInfo, error) {
	return f.info, f.err
}

var _ = Describe("Inspector", func() {
	ctx := context.Background()

	Describe("InspectSystem", func() {
		It("maps the Deployer's SystemInfo through unchanged", func() {
			insp := hardware.NewInspector(fakeInspector{
				info: &redfish.SystemInfo{MemoryGiB: 64, ProcessorCount: 8, Model: "ProLiant"},
			})

			info, err := insp.InspectSystem(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(info.MemoryGiB).To(Equal(64))
			Expect(info.ProcessorCount).To(Equal(8))
			Expect(info.Model).To(Equal("ProLiant"))
		})

		It("wraps the underlying error", func() {
			insp := hardware.NewInspector(fakeInspector{err: errors.New("boom")})
			_, err := insp.InspectSystem(ctx)
			Expect(err).To(MatchError(ContainSubstring("getting system info")))
			Expect(err).To(MatchError(ContainSubstring("boom")))
		})
	})

	Describe("ValidateRequirements", func() {
		insp := hardware.NewInspector(fakeInspector{})

		It("passes when memory and CPU meet the minimums", func() {
			info := &hardware.SystemInfo{MemoryGiB: 16, ProcessorCount: 4}
			reqs := &hardware.Requirements{MinMemoryGiB: 8, MinCPUs: 2}
			Expect(insp.ValidateRequirements(info, reqs)).To(Succeed())
		})

		It("fails when memory is below the minimum", func() {
			info := &hardware.SystemInfo{MemoryGiB: 4, ProcessorCount: 4}
			reqs := &hardware.Requirements{MinMemoryGiB: 8, MinCPUs: 2}
			err := insp.ValidateRequirements(info, reqs)
			Expect(err).To(MatchError(ContainSubstring("insufficient memory")))
		})

		It("fails when CPU count is below the minimum", func() {
			info := &hardware.SystemInfo{MemoryGiB: 16, ProcessorCount: 1}
			reqs := &hardware.Requirements{MinMemoryGiB: 8, MinCPUs: 2}
			err := insp.ValidateRequirements(info, reqs)
			Expect(err).To(MatchError(ContainSubstring("insufficient CPUs")))
		})

		It("passes when a required feature is detected", func() {
			info := &hardware.SystemInfo{
				MemoryGiB:      16,
				ProcessorCount: 4,
				Features:       map[string]bool{"UEFI": true},
			}
			reqs := &hardware.Requirements{RequiredFeatures: []string{"UEFI"}}
			Expect(insp.ValidateRequirements(info, reqs)).To(Succeed())
		})

		It("matches required features case-insensitively", func() {
			info := &hardware.SystemInfo{
				MemoryGiB:      16,
				ProcessorCount: 4,
				Features:       map[string]bool{"UEFI": true},
			}
			reqs := &hardware.Requirements{RequiredFeatures: []string{"uefi"}}
			Expect(insp.ValidateRequirements(info, reqs)).To(Succeed())
		})

		It("fails closed when a known feature was not detected", func() {
			info := &hardware.SystemInfo{
				MemoryGiB:      16,
				ProcessorCount: 4,
				Features:       map[string]bool{}, // UEFI NOT detected
			}
			reqs := &hardware.Requirements{RequiredFeatures: []string{"UEFI"}}
			err := insp.ValidateRequirements(info, reqs)
			Expect(err).To(MatchError(ContainSubstring("not supported by this system")))
			Expect(err).To(MatchError(ContainSubstring("UEFI")))
		})

		It("fails closed with a nil feature map (nothing detected)", func() {
			info := &hardware.SystemInfo{MemoryGiB: 16, ProcessorCount: 4}
			reqs := &hardware.Requirements{RequiredFeatures: []string{"UEFI"}}
			err := insp.ValidateRequirements(info, reqs)
			Expect(err).To(MatchError(ContainSubstring("not supported by this system")))
		})

		It("fails closed on an unknown feature instead of silently passing", func() {
			info := &hardware.SystemInfo{
				MemoryGiB:      16,
				ProcessorCount: 4,
				Features:       map[string]bool{"UEFI": true},
			}
			reqs := &hardware.Requirements{RequiredFeatures: []string{"TELEPATHY"}}
			err := insp.ValidateRequirements(info, reqs)
			Expect(err).To(MatchError(ContainSubstring("not known to AuroraBoot")))
			Expect(err).To(MatchError(ContainSubstring("TELEPATHY")))
		})

		It("detects SecureBoot as a known feature", func() {
			info := &hardware.SystemInfo{
				MemoryGiB:      16,
				ProcessorCount: 4,
				Features:       map[string]bool{"SecureBoot": true},
			}
			reqs := &hardware.Requirements{RequiredFeatures: []string{"SecureBoot"}}
			Expect(insp.ValidateRequirements(info, reqs)).To(Succeed())
		})
	})
})
