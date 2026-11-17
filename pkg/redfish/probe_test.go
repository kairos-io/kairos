package redfish_test

import (
	"context"
	"net/http"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/AuroraBoot/pkg/redfish"
)

var _ = Describe("Deployer.Probe", Label("redfish", "probe"), func() {
	var (
		bmc *fakeBMC
		ctx context.Context
		d   *redfish.Deployer
	)

	newDeployer := func() *redfish.Deployer {
		return redfish.NewDeployer(redfish.Config{
			Endpoint:  bmc.URL(),
			Username:  testUser,
			Password:  testPassword,
			VerifySSL: false,
			Timeout:   30 * time.Second,
		})
	}

	BeforeEach(func() {
		ctx = context.Background()
		bmc = newFakeBMC()
	})

	AfterEach(func() {
		bmc.Close()
	})

	It("reports systems, inspect fields, media, and reset (System-hosted media)", func() {
		bmc.biosVersion = "U30 v2.44 (07/24/2023)"
		d = newDeployer()
		Expect(d.Connect(ctx)).To(Succeed())
		defer func() { _ = d.Close() }()

		report, err := d.Probe(ctx)
		Expect(err).NotTo(HaveOccurred())

		// Service / auth.
		Expect(report.Endpoint).To(Equal(bmc.URL()))
		Expect(report.HasSessionService).To(BeTrue())
		Expect(report.AuthModeUsed).To(Equal("session"))

		// Systems.
		Expect(report.SystemIDs).To(Equal([]string{"sys-xyz"}))
		Expect(report.SelectedSystemID).To(Equal("sys-xyz"))
		Expect(report.MultipleSystems).To(BeFalse())

		// Inspect.
		Expect(report.System.Manufacturer).To(Equal("ACME"))
		Expect(report.System.Model).To(Equal("ProLiant-Test"))
		Expect(report.System.SerialNumber).To(Equal("SN-0001"))
		Expect(report.System.MemoryGiB).To(Equal(64))
		Expect(report.System.ProcessorCount).To(Equal(8))
		Expect(report.System.Features).To(HaveKeyWithValue(redfish.FeatureUEFI, true))
		Expect(report.FirmwareVersion).To(Equal("U30 v2.44 (07/24/2023)"))

		// Media: one System-hosted CD/DVD member, the default search picks it,
		// and it is NOT manager-hosted-only.
		Expect(report.Media).To(HaveLen(1))
		Expect(report.Media[0].Location).To(Equal("system"))
		Expect(report.Media[0].MediaTypes).To(ContainElements("CD", "DVD"))
		Expect(report.DefaultCDIndex).To(Equal(0))
		Expect(report.ManagerHostedCDOnly).To(BeFalse())

		// Reset.
		Expect(report.PowerState).To(Equal("On"))
		Expect(report.AllowableResetTypes).To(ContainElement("ForceRestart"))
		Expect(report.DefaultResetType).To(Equal("ForceRestart"))

		// Read-only: no InsertMedia, boot PATCH, or Reset POST was issued.
		Expect(bmc.sawRequest(http.MethodPost, "/redfish/v1/Systems/sys-xyz/VirtualMedia/Cd/Actions")).To(BeFalse())
		Expect(bmc.sawRequest(http.MethodPatch, "/redfish/v1/Systems/sys-xyz")).To(BeFalse())
		Expect(bmc.sawRequest(http.MethodPost, "/redfish/v1/Systems/sys-xyz/Actions/ComputerSystem.Reset")).To(BeFalse())
		Expect(bmc.insertBody).To(BeNil())
		Expect(bmc.bootPatchBody).To(BeNil())
		Expect(bmc.resetBody).To(BeNil())
	})

	It("flags manager-hosted CD media (the iLO signal)", func() {
		bmc.mediaOnManager = true
		d = newDeployer()
		Expect(d.Connect(ctx)).To(Succeed())
		defer func() { _ = d.Close() }()

		report, err := d.Probe(ctx)
		Expect(err).NotTo(HaveOccurred())

		Expect(report.Media).To(HaveLen(1))
		Expect(report.Media[0].Location).To(HavePrefix("manager:"))
		Expect(report.DefaultCDIndex).To(Equal(0))
		Expect(report.ManagerHostedCDOnly).To(BeTrue())
	})

	It("reports every system Id and flags multiple systems when none is pinned", func() {
		bmc.systemIDs = []string{"sys-a", "sys-b"}
		d = newDeployer()
		Expect(d.Connect(ctx)).To(Succeed())
		defer func() { _ = d.Close() }()

		report, err := d.Probe(ctx)
		Expect(err).NotTo(HaveOccurred())

		Expect(report.SystemIDs).To(ConsistOf("sys-a", "sys-b"))
		Expect(report.MultipleSystems).To(BeTrue())
		// The selected system is the first reported member (gofish does not
		// guarantee collection order, so assert consistency, not a fixed Id).
		Expect(report.SelectedSystemID).To(Equal(report.SystemIDs[0]))
	})

	It("describes the pinned system when --system-id is set", func() {
		bmc.systemIDs = []string{"sys-a", "sys-b"}
		d = redfish.NewDeployer(redfish.Config{
			Endpoint:  bmc.URL(),
			Username:  testUser,
			Password:  testPassword,
			VerifySSL: false,
			Timeout:   30 * time.Second,
			SystemID:  "sys-b",
		})
		Expect(d.Connect(ctx)).To(Succeed())
		defer func() { _ = d.Close() }()

		report, err := d.Probe(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(report.SelectedSystemID).To(Equal("sys-b"))
		Expect(report.MultipleSystems).To(BeFalse())
	})

	It("errors when the pinned system Id is unknown, listing the available Ids", func() {
		bmc.systemIDs = []string{"sys-a", "sys-b"}
		d = redfish.NewDeployer(redfish.Config{
			Endpoint:  bmc.URL(),
			Username:  testUser,
			Password:  testPassword,
			VerifySSL: false,
			Timeout:   30 * time.Second,
			SystemID:  "nope",
		})
		Expect(d.Connect(ctx)).To(Succeed())
		defer func() { _ = d.Close() }()

		_, err := d.Probe(ctx)
		Expect(err).To(MatchError(ContainSubstring("nope")))
		Expect(err).To(MatchError(ContainSubstring("sys-a")))
		Expect(err).To(MatchError(ContainSubstring("sys-b")))
	})

	It("uses basic auth mode in the report when the BMC has no SessionService", func() {
		bmc.noSessionService = true
		d = newDeployer()
		Expect(d.Connect(ctx)).To(Succeed())
		defer func() { _ = d.Close() }()

		report, err := d.Probe(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(report.HasSessionService).To(BeFalse())
		Expect(report.AuthModeUsed).To(Equal("basic"))
	})

	It("reads the SessionService from the ServiceRoot under explicit basic auth", func() {
		d = redfish.NewDeployer(redfish.Config{
			Endpoint:  bmc.URL(),
			Username:  testUser,
			Password:  testPassword,
			VerifySSL: false,
			Timeout:   30 * time.Second,
			AuthMode:  redfish.AuthModeBasic,
		})
		Expect(d.Connect(ctx)).To(Succeed())
		defer func() { _ = d.Close() }()

		report, err := d.Probe(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(report.HasSessionService).To(BeTrue())
		Expect(report.AuthModeUsed).To(Equal("basic"))
	})

	It("reports no SessionService under explicit basic auth when the BMC has none", func() {
		bmc.noSessionService = true
		d = redfish.NewDeployer(redfish.Config{
			Endpoint:  bmc.URL(),
			Username:  testUser,
			Password:  testPassword,
			VerifySSL: false,
			Timeout:   30 * time.Second,
			AuthMode:  redfish.AuthModeBasic,
		})
		Expect(d.Connect(ctx)).To(Succeed())
		defer func() { _ = d.Close() }()

		report, err := d.Probe(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(report.HasSessionService).To(BeFalse())
		Expect(report.AuthModeUsed).To(Equal("basic"))
	})

	It("still probes under explicit basic auth when the ServiceRoot read fails", func() {
		// gofish reads the ServiceRoot once while connecting; fail the second
		// read, the one Probe makes to look for a SessionService.
		bmc.serviceRootFailAfter = 1
		d = redfish.NewDeployer(redfish.Config{
			Endpoint:  bmc.URL(),
			Username:  testUser,
			Password:  testPassword,
			VerifySSL: false,
			Timeout:   30 * time.Second,
			AuthMode:  redfish.AuthModeBasic,
		})
		Expect(d.Connect(ctx)).To(Succeed())
		defer func() { _ = d.Close() }()

		report, err := d.Probe(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(report.HasSessionService).To(BeFalse())
		Expect(report.AuthModeUsed).To(Equal("basic"))
		Expect(report.SystemIDs).NotTo(BeEmpty())
	})

	Describe("StarterProfile round-trips through ParseProfile", func() {
		It("produces a valid profile WITHOUT mediaSearch for System-hosted media", func() {
			bmc.biosVersion = "U30 v2.44"
			d = newDeployer()
			Expect(d.Connect(ctx)).To(Succeed())
			defer func() { _ = d.Close() }()

			report, err := d.Probe(ctx)
			Expect(err).NotTo(HaveOccurred())

			yaml := report.StarterProfile()
			profile, err := redfish.ParseProfile([]byte(yaml))
			Expect(err).NotTo(HaveOccurred())

			Expect(profile.Name).To(Equal("acme"))
			Expect(profile.Match).NotTo(BeNil())
			Expect(profile.Match.Vendor).To(Equal("ACME"))
			Expect(profile.MediaSearch).To(BeNil(), "System-hosted media needs no mediaSearch")
			Expect(profile.ValidatedFirmware).To(Equal("U30 v2.44"))
			// resetType suggestions are comments, never active rules.
			Expect(profile.ResetType).To(BeEmpty())
		})

		It("produces a valid profile WITH a manager-first mediaSearch for manager-hosted media", func() {
			bmc.mediaOnManager = true
			d = newDeployer()
			Expect(d.Connect(ctx)).To(Succeed())
			defer func() { _ = d.Close() }()

			report, err := d.Probe(ctx)
			Expect(err).NotTo(HaveOccurred())

			yaml := report.StarterProfile()
			Expect(yaml).To(ContainSubstring("mediaSearch"))

			profile, err := redfish.ParseProfile([]byte(yaml))
			Expect(err).NotTo(HaveOccurred())
			Expect(profile.MediaSearch).NotTo(BeNil())
			Expect(profile.MediaSearch.Order).To(Equal([]string{"manager", "system"}))
		})

		It("slugs to \"custom\" when the BMC reports no manufacturer", func() {
			report := &redfish.ProbeReport{} // empty manufacturer
			yaml := report.StarterProfile()
			Expect(strings.Contains(yaml, "name: custom")).To(BeTrue())

			profile, err := redfish.ParseProfile([]byte(yaml))
			Expect(err).NotTo(HaveOccurred())
			Expect(profile.Name).To(Equal("custom"))
			Expect(profile.Match).To(BeNil())
		})
	})
})
