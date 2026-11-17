package redfish_test

import (
	"context"
	"net/http"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/AuroraBoot/pkg/redfish"
)

// This is the P4 per-vendor golden test (design §4a). For each (mockup, profile)
// pair it replays the FULL deploy flow — discover → InsertMedia → boot → reset →
// task → teardown — against a vendor's recorded DMTF GET tree (served by mockupBMC)
// and asserts the request sequence the profile must produce. The recorded responses
// are that vendor's REAL GET bodies (sanitized, hand-authored here for iLO), so the
// profile is exercised against real-shaped hardware with no metal in CI.
//
// Adding the next vendor is one new row: a recorded testdata/mockups/<vendor>/<fw>
// tree and the profile selected for it. See testdata/mockups/README.md.

// goldenCase is one (recorded mockup, selected profile) pair plus the request
// sequence the profile is expected to produce against that tree.
type goldenCase struct {
	name string
	// mockupDir is the recorded tree root (the dir holding the "redfish" folder).
	mockupDir string
	// vendor selects the in-tree quirk profile under test.
	vendor redfish.VendorType
	// wantSystemID / wantMediaID are the discovered Ids the deploy must resolve.
	wantSystemID string
	wantMediaID  string
	// wantInsertPath is the path prefix the InsertMedia POST must hit, proving the
	// mediaSearch order picked the right (e.g. Manager-hosted) member.
	wantInsertPath string
	// wantResetType is the ResetType the profile + firstSupported must select from
	// the recorded allowable values.
	wantResetType string
}

var _ = Describe("Recorded-mockup golden replay", func() {
	const (
		goldenUser     = "admin"
		goldenPassword = "s3cr3t-p@ss"
		goldenImageURL = "http://serve.example/kairos.iso"
		iloMockup      = "testdata/mockups/ilo/gen10-ilo5-fw2.44"
	)

	cases := []goldenCase{
		{
			name:           "ilo (HPE) — Manager-hosted CD selected by mediaSearch",
			mockupDir:      iloMockup,
			vendor:         redfish.VendorHPE,
			wantSystemID:   "1",
			wantMediaID:    "2", // the CD/DVD member (member 1 is Floppy/USBStick)
			wantInsertPath: "/redfish/v1/Managers/1/VirtualMedia/2/Actions/VirtualMedia.InsertMedia",
			wantResetType:  "ForceRestart",
		},
	}

	for _, tc := range cases {
		tc := tc
		It(tc.name, func() {
			bmc := newMockupBMC(tc.mockupDir)
			defer bmc.Close()

			ctx := context.Background()
			d := redfish.NewDeployer(redfish.Config{
				Endpoint:  bmc.URL(),
				Username:  goldenUser,
				Password:  goldenPassword,
				Vendor:    tc.vendor,
				VerifySSL: false, // mockup server uses a self-signed TLS cert
				Timeout:   30 * time.Second,
			})
			Expect(d.Connect(ctx)).To(Succeed())

			result, err := d.Deploy(ctx, redfish.DeployRequest{
				ImageURL:   goldenImageURL,
				BootTarget: redfish.BootTargetCd,
				// BootMode left empty: the deploy must NOT force a firmware mode.
			})
			Expect(err).NotTo(HaveOccurred())

			// Discovery resolved the expected system and the expected media member —
			// for iLO this proves mediaSearch.order picked the Manager-hosted CD over
			// the Floppy/USB member and over the (absent) System media.
			Expect(result.SystemID).To(Equal(tc.wantSystemID))
			Expect(result.MediaID).To(Equal(tc.wantMediaID))

			// InsertMedia hit the chosen member's action, and the body's Image is the
			// harness's served URL UNCHANGED — proving the profile never rewrote it
			// (the Image URL is core-owned and SSRF-validated; no profile can touch it).
			Expect(bmc.sawRequest(http.MethodPost, tc.wantInsertPath)).To(BeTrue(),
				"InsertMedia must hit the profile-selected media member")
			Expect(bmc.insertBody()).To(HaveKeyWithValue("Image", goldenImageURL))
			Expect(bmc.insertBody()).To(HaveKeyWithValue("Inserted", true))
			Expect(bmc.insertBody()).To(HaveKeyWithValue("WriteProtected", true))

			// Boot PATCH: one-time override to Cd, and — with no BootMode requested —
			// NO BootSourceOverrideMode (forcing the firmware mode breaks some BMCs).
			boot, ok := bmc.bootPatchBody()["Boot"].(map[string]any)
			Expect(ok).To(BeTrue())
			Expect(boot).To(HaveKeyWithValue("BootSourceOverrideEnabled", "Once"))
			Expect(boot).To(HaveKeyWithValue("BootSourceOverrideTarget", "Cd"))
			Expect(boot).NotTo(HaveKey("BootSourceOverrideMode"))

			// Reset carried the ResetType firstSupported chose from the RECORDED
			// allowable values (the system is "On", so ForceRestart, which the
			// recorded tree advertises).
			Expect(bmc.resetBody()).To(HaveKeyWithValue("ResetType", tc.wantResetType))
			Expect(bmc.resetSystemID()).To(Equal(tc.wantSystemID))

			// Async Task polled to a terminal Completed state.
			Expect(result.TaskCompleted).To(BeTrue())
			Expect(result.TaskState).To(Equal("Completed"))

			// Teardown invariant: the session is DELETEd on Close even though the GET
			// tree was recorded (session create/delete are synthesized + recorded).
			Expect(d.Close()).To(Succeed())
			Expect(bmc.sawRequest(http.MethodDelete, bmc.sessionLocation())).To(BeTrue(),
				"the session must be torn down on Close")
		})
	}

	// Negative guard / regression value: the profile must actually change the outcome
	// on a recorded tree. We use a second recorded iLO-shaped tree where BOTH the
	// System and the Manager expose a CD/DVD member. The spec-default (generic) flat
	// order is System-first, so generic selects the System CD; the iLO profile's
	// manager-first mediaSearch selects the Manager CD instead. Same recorded tree,
	// two profiles, two different InsertMedia targets — proving the profile, not the
	// harness, drives the selection (this is the regression value of the golden test).
	It("(negative guard) generic and ilo pick DIFFERENT media on the same recorded tree", func() {
		const dualMockup = "testdata/mockups/ilo/system-and-manager-cd"
		ctx := context.Background()

		newDeployer := func(vendor redfish.VendorType) (*redfish.Deployer, *mockupBMC) {
			bmc := newMockupBMC(dualMockup)
			d := redfish.NewDeployer(redfish.Config{
				Endpoint: bmc.URL(), Username: goldenUser, Password: goldenPassword,
				Vendor: vendor, VerifySSL: false, Timeout: 30 * time.Second,
			})
			return d, bmc
		}

		// generic: spec-default System-first — selects the System-hosted CD.
		gd, gbmc := newDeployer(redfish.VendorGeneric)
		defer gbmc.Close()
		Expect(gd.Connect(ctx)).To(Succeed())
		gres, err := gd.Deploy(ctx, redfish.DeployRequest{ImageURL: goldenImageURL, BootTarget: redfish.BootTargetCd})
		Expect(err).NotTo(HaveOccurred())
		Expect(gbmc.sawRequest(http.MethodPost,
			"/redfish/v1/Systems/1/VirtualMedia/Cd/Actions/VirtualMedia.InsertMedia")).To(BeTrue(),
			"generic must pick the System-hosted CD (spec-default System-first order)")
		Expect(gd.Close()).To(Succeed())

		// ilo: manager-first mediaSearch — selects the Manager-hosted CD instead.
		id, ibmc := newDeployer(redfish.VendorHPE)
		defer ibmc.Close()
		Expect(id.Connect(ctx)).To(Succeed())
		ires, err := id.Deploy(ctx, redfish.DeployRequest{ImageURL: goldenImageURL, BootTarget: redfish.BootTargetCd})
		Expect(err).NotTo(HaveOccurred())
		Expect(ibmc.sawRequest(http.MethodPost,
			"/redfish/v1/Managers/1/VirtualMedia/2/Actions/VirtualMedia.InsertMedia")).To(BeTrue(),
			"ilo must pick the Manager-hosted CD (manager-first mediaSearch order)")
		Expect(id.Close()).To(Succeed())

		// The two profiles demonstrably diverged on the SAME recorded tree.
		Expect(gres.MediaID).NotTo(Equal(ires.MediaID))
	})
})
