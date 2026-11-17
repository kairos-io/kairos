package redfish_test

import (
	"context"
	"errors"
	"net/http"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/AuroraBoot/pkg/redfish"
)

const (
	testUser     = "admin"
	testPassword = "s3cr3t-p@ss"
	testImageURL = "http://serve.example/kairos.iso"
)

var _ = Describe("Deployer", func() {
	var (
		bmc *fakeBMC
		ctx context.Context
		d   *redfish.Deployer
	)

	BeforeEach(func() {
		ctx = context.Background()
		bmc = newFakeBMC()
		d = redfish.NewDeployer(redfish.Config{
			Endpoint:  bmc.URL(),
			Username:  testUser,
			Password:  testPassword,
			VerifySSL: false, // fake BMC uses a self-signed TLS cert
			Timeout:   30 * time.Second,
		})
	})

	AfterEach(func() {
		bmc.Close()
	})

	Describe("Connect and Close", func() {
		It("creates a session with a JSON credential body and deletes it on Close", func() {
			Expect(d.Connect(ctx)).To(Succeed())

			// Session was created with a JSON body carrying the credentials.
			Expect(bmc.sessionBody).To(HaveKeyWithValue("UserName", testUser))
			Expect(bmc.sessionBody).To(HaveKeyWithValue("Password", testPassword))

			Expect(d.Close()).To(Succeed())
			Expect(bmc.sawRequest(http.MethodDelete, bmc.sessionLocation)).To(BeTrue(),
				"Close must DELETE the session resource")
		})

		It("rejects a second Connect", func() {
			Expect(d.Connect(ctx)).To(Succeed())
			defer func() { _ = d.Close() }()
			Expect(d.Connect(ctx)).To(MatchError(ContainSubstring("already connected")))
		})
	})

	Describe("Inspect", func() {
		It("populates memory and CPU from the nested summaries (no 0/0 bug)", func() {
			Expect(d.Connect(ctx)).To(Succeed())
			defer func() { _ = d.Close() }()

			info, err := d.Inspect(ctx)
			Expect(err).NotTo(HaveOccurred())

			// These come from MemorySummary.TotalSystemMemoryGiB / ProcessorSummary.Count.
			Expect(info.MemoryGiB).To(Equal(64))
			Expect(info.ProcessorCount).To(Equal(8))
			Expect(info.Manufacturer).To(Equal("ACME"))
			Expect(info.Model).To(Equal("ProLiant-Test"))
			Expect(info.SerialNumber).To(Equal("SN-0001"))
			// Discovery used the opaque member ID, not a hardcoded Systems/1.
			Expect(info.ID).To(Equal("sys-xyz"))
		})

		It("detects UEFI from the boot-mode allowable values", func() {
			bmc.bootModeAllowableValues = []string{"Legacy", "UEFI"}
			Expect(d.Connect(ctx)).To(Succeed())
			defer func() { _ = d.Close() }()

			info, err := d.Inspect(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(info.Features).To(HaveKeyWithValue(redfish.FeatureUEFI, true))
		})

		It("does not report UEFI when the allowable boot modes exclude it", func() {
			bmc.bootModeAllowableValues = []string{"Legacy"}
			Expect(d.Connect(ctx)).To(Succeed())
			defer func() { _ = d.Close() }()

			info, err := d.Inspect(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(info.Features).NotTo(HaveKey(redfish.FeatureUEFI))
		})

		It("does not report UEFI when no boot-mode signal is present", func() {
			bmc.bootModeAllowableValues = nil // BMC omits the annotation
			Expect(d.Connect(ctx)).To(Succeed())
			defer func() { _ = d.Close() }()

			info, err := d.Inspect(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(info.Features).NotTo(HaveKey(redfish.FeatureUEFI))
		})

		It("detects SecureBoot only when the system exposes the link", func() {
			bmc.withSecureBoot = true
			Expect(d.Connect(ctx)).To(Succeed())
			defer func() { _ = d.Close() }()

			info, err := d.Inspect(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(info.Features).To(HaveKeyWithValue(redfish.FeatureSecureBoot, true))
		})

		It("does not report SecureBoot when no link is present", func() {
			bmc.withSecureBoot = false
			Expect(d.Connect(ctx)).To(Succeed())
			defer func() { _ = d.Close() }()

			info, err := d.Inspect(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(info.Features).NotTo(HaveKey(redfish.FeatureSecureBoot))
		})
	})

	Describe("Deploy happy path", func() {
		It("inserts media, sets one-time boot, resets with a ResetType, polls the task, and tears down the session", func() {
			Expect(d.Connect(ctx)).To(Succeed())

			result, err := d.Deploy(ctx, redfish.DeployRequest{
				ImageURL:   testImageURL,
				BootTarget: redfish.BootTargetCd,
				// BootMode left empty: the default must NOT force a firmware mode.
			})
			Expect(err).NotTo(HaveOccurred())

			// Discovery used opaque IDs, never hardcoded Systems/1 or VirtualMedia/1.
			Expect(result.SystemID).To(Equal("sys-xyz"))
			Expect(result.MediaID).To(Equal("Cd"))
			Expect(bmc.sawRequest(http.MethodGet, "/redfish/v1/Systems/sys-xyz")).To(BeTrue())

			// InsertMedia URL-pull body.
			Expect(bmc.insertBody).To(HaveKeyWithValue("Image", testImageURL))
			Expect(bmc.insertBody).To(HaveKeyWithValue("Inserted", true))
			Expect(bmc.insertBody).To(HaveKeyWithValue("WriteProtected", true))

			// One-time boot override PATCH: Once / Cd, and — with no BootMode set —
			// the PATCH must NOT carry BootSourceOverrideMode (forcing the firmware
			// mode is what breaks some BMCs/emulators).
			Expect(bmc.bootPatchBody).To(HaveKey("Boot"))
			boot, ok := bmc.bootPatchBody["Boot"].(map[string]any)
			Expect(ok).To(BeTrue())
			Expect(boot).To(HaveKeyWithValue("BootSourceOverrideEnabled", "Once"))
			Expect(boot).To(HaveKeyWithValue("BootSourceOverrideTarget", "Cd"))
			Expect(boot).NotTo(HaveKey("BootSourceOverrideMode"))

			// Reset carried an explicit ResetType.
			Expect(bmc.resetBody).To(HaveKey("ResetType"))
			Expect(bmc.resetBody["ResetType"]).NotTo(BeEmpty())

			// Async Task was polled to a terminal Completed state.
			Expect(result.TaskCompleted).To(BeTrue())
			Expect(result.TaskState).To(Equal("Completed"))

			// Session is deleted on Close even on the success path.
			Expect(d.Close()).To(Succeed())
			Expect(bmc.sawRequest(http.MethodDelete, bmc.sessionLocation)).To(BeTrue())
		})

		It("never ejects media during Deploy, even with the deprecated EjectAfter set", func() {
			// The mid-install eject is GONE: ejecting at deploy-Task completion is the
			// wrong moment (the install loop). Setting the deprecated EjectAfter must
			// have no effect — eject is now Finalize's job.
			Expect(d.Connect(ctx)).To(Succeed())
			defer func() { _ = d.Close() }()

			_, err := d.Deploy(ctx, redfish.DeployRequest{
				ImageURL:   testImageURL,
				BootTarget: redfish.BootTargetCd,
				EjectAfter: true, // deprecated, must be ignored
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(bmc.ejectCalled).To(BeFalse())
			Expect(bmc.sawRequest(http.MethodPost, "/redfish/v1/Systems/sys-xyz/VirtualMedia/Cd/Actions/VirtualMedia.EjectMedia")).To(BeFalse())
		})

		It("sends BootSourceOverrideMode=UEFI when BootModeUEFI is explicitly requested", func() {
			Expect(d.Connect(ctx)).To(Succeed())
			defer func() { _ = d.Close() }()

			_, err := d.Deploy(ctx, redfish.DeployRequest{
				ImageURL:   testImageURL,
				BootTarget: redfish.BootTargetCd,
				BootMode:   redfish.BootModeUEFI,
			})
			Expect(err).NotTo(HaveOccurred())

			boot, ok := bmc.bootPatchBody["Boot"].(map[string]any)
			Expect(ok).To(BeTrue())
			Expect(boot).To(HaveKeyWithValue("BootSourceOverrideEnabled", "Once"))
			Expect(boot).To(HaveKeyWithValue("BootSourceOverrideTarget", "Cd"))
			Expect(boot).To(HaveKeyWithValue("BootSourceOverrideMode", "UEFI"))
		})

		It("sends BootSourceOverrideMode=Legacy when BootModeLegacy is explicitly requested", func() {
			Expect(d.Connect(ctx)).To(Succeed())
			defer func() { _ = d.Close() }()

			_, err := d.Deploy(ctx, redfish.DeployRequest{
				ImageURL:   testImageURL,
				BootTarget: redfish.BootTargetCd,
				BootMode:   redfish.BootModeLegacy,
			})
			Expect(err).NotTo(HaveOccurred())

			boot, ok := bmc.bootPatchBody["Boot"].(map[string]any)
			Expect(ok).To(BeTrue())
			Expect(boot).To(HaveKeyWithValue("BootSourceOverrideMode", "Legacy"))
		})
	})

	Describe("Deploy progress callback", func() {
		It("invokes the callback in non-decreasing percent order ending at 100", func() {
			Expect(d.Connect(ctx)).To(Succeed())
			defer func() { _ = d.Close() }()

			var steps []string
			var percents []int
			_, err := d.Deploy(ctx, redfish.DeployRequest{
				ImageURL:   testImageURL,
				BootTarget: redfish.BootTargetCd,
				BootMode:   redfish.BootModeUEFI,
				Progress: func(step string, percent int) {
					steps = append(steps, step)
					percents = append(percents, percent)
				},
			})
			Expect(err).NotTo(HaveOccurred())

			// The flow reports each stage.
			Expect(steps).To(ContainElements(
				"discovering", "inserting media", "setting boot", "resetting", "polling task", "completed",
			))

			// Percentages are monotonically non-decreasing and finish at 100.
			Expect(percents).NotTo(BeEmpty())
			for i := 1; i < len(percents); i++ {
				Expect(percents[i]).To(BeNumerically(">=", percents[i-1]),
					"percent must never regress")
			}
			Expect(percents[0]).To(Equal(10))
			Expect(percents[len(percents)-1]).To(Equal(100))
		})

		It("is nil-safe (no callback supplied)", func() {
			Expect(d.Connect(ctx)).To(Succeed())
			defer func() { _ = d.Close() }()

			_, err := d.Deploy(ctx, redfish.DeployRequest{
				ImageURL:   testImageURL,
				BootTarget: redfish.BootTargetCd,
				BootMode:   redfish.BootModeUEFI,
				// Progress nil
			})
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("Deploy error path", func() {
		It("still tears the session down when InsertMedia fails mid-flow", func() {
			bmc.insertMediaStatus = http.StatusInternalServerError

			Expect(d.Connect(ctx)).To(Succeed())

			_, err := d.Deploy(ctx, redfish.DeployRequest{
				ImageURL:   testImageURL,
				BootTarget: redfish.BootTargetCd,
				BootMode:   redfish.BootModeUEFI,
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("inserting virtual media"))

			// deferred Close still runs on the error path.
			Expect(d.Close()).To(Succeed())
			Expect(bmc.sawRequest(http.MethodDelete, bmc.sessionLocation)).To(BeTrue(),
				"the session must be DELETEd even when the deploy errors")
		})

		It("scrubs the password from returned errors", func() {
			bmc.insertMediaStatus = http.StatusInternalServerError

			Expect(d.Connect(ctx)).To(Succeed())
			defer func() { _ = d.Close() }()

			_, err := d.Deploy(ctx, redfish.DeployRequest{ImageURL: testImageURL})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).NotTo(ContainSubstring(testPassword))
		})

		It("requires an ImageURL", func() {
			Expect(d.Connect(ctx)).To(Succeed())
			defer func() { _ = d.Close() }()

			_, err := d.Deploy(ctx, redfish.DeployRequest{})
			Expect(err).To(MatchError(ContainSubstring("ImageURL is required")))
		})
	})

	Describe("System selection (fail-safe)", func() {
		Context("with more than one ComputerSystem and no SystemID", func() {
			BeforeEach(func() {
				bmc.systemIDs = []string{"node-a", "node-b"}
			})

			It("refuses to Inspect and lists the available system Ids", func() {
				Expect(d.Connect(ctx)).To(Succeed())
				defer func() { _ = d.Close() }()

				_, err := d.Inspect(ctx)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("selection is required"))
				Expect(err.Error()).To(ContainSubstring("node-a"))
				Expect(err.Error()).To(ContainSubstring("node-b"))
			})

			It("refuses to Deploy (does not reset any machine) and lists the available Ids", func() {
				Expect(d.Connect(ctx)).To(Succeed())
				defer func() { _ = d.Close() }()

				_, err := d.Deploy(ctx, redfish.DeployRequest{
					ImageURL:   testImageURL,
					BootTarget: redfish.BootTargetCd,
					BootMode:   redfish.BootModeUEFI,
				})
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("selection is required"))
				Expect(err.Error()).To(ContainSubstring("node-a"))
				Expect(err.Error()).To(ContainSubstring("node-b"))

				// Critically: no system was reset, no media inserted.
				Expect(bmc.sawRequest(http.MethodPost,
					"/redfish/v1/Systems/node-a/Actions/ComputerSystem.Reset")).To(BeFalse())
				Expect(bmc.sawRequest(http.MethodPost,
					"/redfish/v1/Systems/node-b/Actions/ComputerSystem.Reset")).To(BeFalse())
				Expect(bmc.resetSystemID).To(BeEmpty())
			})
		})

		Context("with more than one ComputerSystem and a valid SystemID", func() {
			It("targets exactly the selected system on Inspect and Deploy", func() {
				bmc.systemIDs = []string{"node-a", "node-b"}

				sel := redfish.NewDeployer(redfish.Config{
					Endpoint:  bmc.URL(),
					Username:  testUser,
					Password:  testPassword,
					VerifySSL: false,
					Timeout:   30 * time.Second,
					SystemID:  "node-b",
				})
				Expect(sel.Connect(ctx)).To(Succeed())
				defer func() { _ = sel.Close() }()

				info, err := sel.Inspect(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(info.ID).To(Equal("node-b"))

				result, err := sel.Deploy(ctx, redfish.DeployRequest{
					ImageURL:   testImageURL,
					BootTarget: redfish.BootTargetCd,
					BootMode:   redfish.BootModeUEFI,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(result.SystemID).To(Equal("node-b"))

				// The InsertMedia and Reset hit node-b's resources, not node-a's.
				Expect(bmc.sawRequest(http.MethodPost,
					"/redfish/v1/Systems/node-b/VirtualMedia/Cd/Actions/VirtualMedia.InsertMedia")).To(BeTrue())
				Expect(bmc.sawRequest(http.MethodPost,
					"/redfish/v1/Systems/node-b/Actions/ComputerSystem.Reset")).To(BeTrue())
				Expect(bmc.resetSystemID).To(Equal("node-b"))

				// node-a was never touched.
				Expect(bmc.sawRequest(http.MethodPost,
					"/redfish/v1/Systems/node-a/Actions/ComputerSystem.Reset")).To(BeFalse())
			})
		})

		Context("with a SystemID that matches nothing", func() {
			It("errors listing the available system Ids", func() {
				bmc.systemIDs = []string{"node-a", "node-b"}

				sel := redfish.NewDeployer(redfish.Config{
					Endpoint:  bmc.URL(),
					Username:  testUser,
					Password:  testPassword,
					VerifySSL: false,
					Timeout:   30 * time.Second,
					SystemID:  "does-not-exist",
				})
				Expect(sel.Connect(ctx)).To(Succeed())
				defer func() { _ = sel.Close() }()

				_, err := sel.Inspect(ctx)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring(`no ComputerSystem with Id "does-not-exist"`))
				Expect(err.Error()).To(ContainSubstring("node-a"))
				Expect(err.Error()).To(ContainSubstring("node-b"))
			})
		})

		Context("with a single ComputerSystem and no SystemID (back-compat)", func() {
			It("Inspects and Deploys against the sole member", func() {
				// bmc defaults to a single "sys-xyz" system.
				Expect(d.Connect(ctx)).To(Succeed())
				defer func() { _ = d.Close() }()

				info, err := d.Inspect(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(info.ID).To(Equal("sys-xyz"))

				result, err := d.Deploy(ctx, redfish.DeployRequest{
					ImageURL:   testImageURL,
					BootTarget: redfish.BootTargetCd,
					BootMode:   redfish.BootModeUEFI,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(result.SystemID).To(Equal("sys-xyz"))
				Expect(bmc.resetSystemID).To(Equal("sys-xyz"))
			})
		})
	})

	Describe("VirtualMedia discovery on the Manager", func() {
		It("finds CD media on the Manager when the System has none", func() {
			bmc.mediaOnManager = true

			Expect(d.Connect(ctx)).To(Succeed())
			defer func() { _ = d.Close() }()

			result, err := d.Deploy(ctx, redfish.DeployRequest{
				ImageURL:   testImageURL,
				BootTarget: redfish.BootTargetCd,
				BootMode:   redfish.BootModeUEFI,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.MediaID).To(Equal("Cd"))
			Expect(bmc.sawRequest(http.MethodPost,
				"/redfish/v1/Managers/mgr-1/VirtualMedia/Cd/Actions/VirtualMedia.InsertMedia")).To(BeTrue())
		})
	})

	Describe("Vendor quirk seam", func() {
		// Regression guard: the generic profile must drive the exact same protocol
		// flow as the default. We run the happy path with Vendor explicitly set to
		// generic and assert the same request shapes the default path produces.
		It("leaves the generic path byte-for-byte unchanged", func() {
			gd := redfish.NewDeployer(redfish.Config{
				Endpoint:  bmc.URL(),
				Username:  testUser,
				Password:  testPassword,
				Vendor:    redfish.VendorGeneric,
				VerifySSL: false,
				Timeout:   30 * time.Second,
			})
			Expect(gd.Connect(ctx)).To(Succeed())
			defer func() { _ = gd.Close() }()

			_, err := gd.Deploy(ctx, redfish.DeployRequest{
				ImageURL:   testImageURL,
				BootTarget: redfish.BootTargetCd,
				BootMode:   redfish.BootModeUEFI,
			})
			Expect(err).NotTo(HaveOccurred())

			// Same InsertMedia URL-pull body as the default path.
			Expect(bmc.insertBody).To(HaveKeyWithValue("Image", testImageURL))
			Expect(bmc.insertBody).To(HaveKeyWithValue("Inserted", true))
			Expect(bmc.insertBody).To(HaveKeyWithValue("WriteProtected", true))
			Expect(bmc.insertBody).To(HaveKeyWithValue("MediaType", "CD"))
			// Discovery hit the System media first (default order), not the Manager.
			Expect(bmc.sawRequest(http.MethodPost,
				"/redfish/v1/Systems/sys-xyz/VirtualMedia/Cd/Actions/VirtualMedia.InsertMedia")).To(BeTrue())
		})

		It("(ilo) discovers Manager-hosted media when the System exposes none", func() {
			bmc.mediaOnManager = true

			ilo := redfish.NewDeployer(redfish.Config{
				Endpoint:  bmc.URL(),
				Username:  testUser,
				Password:  testPassword,
				Vendor:    redfish.VendorHPE,
				VerifySSL: false,
				Timeout:   30 * time.Second,
			})
			Expect(ilo.Connect(ctx)).To(Succeed())
			defer func() { _ = ilo.Close() }()

			result, err := ilo.Deploy(ctx, redfish.DeployRequest{
				ImageURL:   testImageURL,
				BootTarget: redfish.BootTargetCd,
				BootMode:   redfish.BootModeUEFI,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.MediaID).To(Equal("Cd"))
			Expect(bmc.sawRequest(http.MethodPost,
				"/redfish/v1/Managers/mgr-1/VirtualMedia/Cd/Actions/VirtualMedia.InsertMedia")).To(BeTrue())
		})
	})

	Describe("Redfish error surfacing", func() {
		It("includes @Message.ExtendedInfo detail in the returned error", func() {
			bmc.insertMediaStatus = http.StatusBadRequest
			bmc.insertMediaExtendedInfo = true

			Expect(d.Connect(ctx)).To(Succeed())
			defer func() { _ = d.Close() }()

			_, err := d.Deploy(ctx, redfish.DeployRequest{
				ImageURL:   testImageURL,
				BootTarget: redfish.BootTargetCd,
				BootMode:   redfish.BootModeUEFI,
			})
			Expect(err).To(HaveOccurred())
			// The actionable ExtendedInfo text and resolution must surface, not just
			// a bare status code.
			Expect(err.Error()).To(ContainSubstring("inserting virtual media"))
			Expect(err.Error()).To(ContainSubstring("The image URL could not be reached by the BMC."))
			Expect(err.Error()).To(ContainSubstring("Base.1.0.ResourceAtUriUnauthorized"))
			Expect(err.Error()).To(ContainSubstring("Verify the image URL is reachable"))
		})
	})

	Describe("Unknown TaskState handling", func() {
		It("fails fast on a garbage TaskState instead of looping forever", func() {
			// The Task GET will return an out-of-enum state; the poll must reject it.
			bmc.taskStates = []string{"Frobnicating"}

			Expect(d.Connect(ctx)).To(Succeed())
			defer func() { _ = d.Close() }()

			// Bound the test independently so a regression (infinite loop) fails
			// loudly rather than hanging the suite.
			tctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()

			_, err := d.Deploy(tctx, redfish.DeployRequest{
				ImageURL:   testImageURL,
				BootTarget: redfish.BootTargetCd,
				BootMode:   redfish.BootModeUEFI,
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unknown Redfish TaskState"))
			Expect(err.Error()).To(ContainSubstring("Frobnicating"))
		})
	})

	Describe("Timeout", func() {
		It("stops polling a task that never leaves Running once Config.Timeout runs out", func() {
			bmc.taskStates = []string{"Running"}
			d = redfish.NewDeployer(redfish.Config{
				Endpoint:  bmc.URL(),
				Username:  testUser,
				Password:  testPassword,
				VerifySSL: false,
				Timeout:   time.Second,
			})

			Expect(d.Connect(ctx)).To(Succeed())

			// ctx has no deadline: only Config.Timeout can end the poll. The
			// outer bound keeps a regression from hanging the suite.
			done := make(chan error, 1)
			start := time.Now()
			go func() {
				_, err := d.Deploy(ctx, redfish.DeployRequest{
					ImageURL:   testImageURL,
					BootTarget: redfish.BootTargetCd,
					BootMode:   redfish.BootModeUEFI,
				})
				done <- err
			}()

			var err error
			Eventually(done, 10*time.Second).Should(Receive(&err))
			Expect(err).To(MatchError(ContainSubstring("did not finish within the 1s timeout")))
			Expect(errors.Is(err, context.DeadlineExceeded)).To(BeTrue())
			Expect(time.Since(start)).To(BeNumerically("<", 3*time.Second))

			// The session is still torn down after the deadline.
			Expect(d.Close()).To(Succeed())
			Expect(bmc.sawRequest(http.MethodDelete, bmc.sessionLocation)).To(BeTrue())
		})
	})

	Describe("Authentication mode", func() {
		Context("auto (default) against an endpoint WITH a SessionService", func() {
			It("uses session auth and DELETEs the session on Close", func() {
				// d defaults to AuthMode "" -> auto; bmc advertises a SessionService.
				Expect(d.Connect(ctx)).To(Succeed())

				// A session was created (JSON credential body captured).
				Expect(bmc.sessionBody).To(HaveKeyWithValue("UserName", testUser))

				_, err := d.Deploy(ctx, redfish.DeployRequest{
					ImageURL:   testImageURL,
					BootTarget: redfish.BootTargetCd,
					BootMode:   redfish.BootModeUEFI,
				})
				Expect(err).NotTo(HaveOccurred())

				Expect(d.Close()).To(Succeed())
				Expect(bmc.sawRequest(http.MethodDelete, bmc.sessionLocation)).To(BeTrue(),
					"auto with a SessionService must DELETE the session")
			})
		})

		Context("auto against an endpoint WITHOUT a SessionService", func() {
			BeforeEach(func() {
				bmc.noSessionService = true
			})

			It("falls back to Basic auth, deploys, and does NOT DELETE a session on Close", func() {
				Expect(d.Connect(ctx)).To(Succeed())

				// No session was ever created (no JSON credential body, no token).
				Expect(bmc.sessionBody).To(BeNil())

				// The full deploy still works over Basic auth.
				result, err := d.Deploy(ctx, redfish.DeployRequest{
					ImageURL:   testImageURL,
					BootTarget: redfish.BootTargetCd,
					BootMode:   redfish.BootModeUEFI,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(result.TaskState).To(Equal("Completed"))

				// Requests carried HTTP Basic auth, not a session token.
				Expect(bmc.sawAuthType("Basic", "/redfish/v1/Systems")).To(BeTrue(),
					"basic-auth fallback must send Authorization: Basic on requests")
				Expect(bmc.sawAuthType("Token", "/redfish/v1/Systems")).To(BeFalse())

				// Close is a no-op for Basic auth: no session DELETE, no error.
				Expect(d.Close()).To(Succeed())
				Expect(bmc.sawRequest(http.MethodDelete, bmc.sessionLocation)).To(BeFalse(),
					"basic auth has no session to DELETE")
			})
		})

		Context("explicit basic even when a SessionService exists", func() {
			It("uses Basic auth and never creates a session", func() {
				bd := redfish.NewDeployer(redfish.Config{
					Endpoint:  bmc.URL(),
					Username:  testUser,
					Password:  testPassword,
					VerifySSL: false,
					Timeout:   30 * time.Second,
					AuthMode:  redfish.AuthModeBasic,
				})
				Expect(bd.Connect(ctx)).To(Succeed())

				_, err := bd.Deploy(ctx, redfish.DeployRequest{
					ImageURL:   testImageURL,
					BootTarget: redfish.BootTargetCd,
					BootMode:   redfish.BootModeUEFI,
				})
				Expect(err).NotTo(HaveOccurred())

				// Despite a SessionService being available, no session was created.
				Expect(bmc.sessionBody).To(BeNil())
				Expect(bmc.sawAuthType("Basic", "/redfish/v1/Systems")).To(BeTrue())

				Expect(bd.Close()).To(Succeed())
				Expect(bmc.sawRequest(http.MethodDelete, bmc.sessionLocation)).To(BeFalse())
			})
		})

		Context("explicit session", func() {
			It("uses session auth and DELETEs it on Close (current behaviour)", func() {
				sd := redfish.NewDeployer(redfish.Config{
					Endpoint:  bmc.URL(),
					Username:  testUser,
					Password:  testPassword,
					VerifySSL: false,
					Timeout:   30 * time.Second,
					AuthMode:  redfish.AuthModeSession,
				})
				Expect(sd.Connect(ctx)).To(Succeed())
				Expect(bmc.sessionBody).To(HaveKeyWithValue("UserName", testUser))

				Expect(sd.Close()).To(Succeed())
				Expect(bmc.sawRequest(http.MethodDelete, bmc.sessionLocation)).To(BeTrue())
			})
		})

		Context("unknown auth mode", func() {
			It("rejects Connect with a clear error", func() {
				ud := redfish.NewDeployer(redfish.Config{
					Endpoint:  bmc.URL(),
					Username:  testUser,
					Password:  testPassword,
					VerifySSL: false,
					Timeout:   30 * time.Second,
					AuthMode:  redfish.AuthMode("nonsense"),
				})
				err := ud.Connect(ctx)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("unknown Redfish auth mode"))
				Expect(err.Error()).To(ContainSubstring("nonsense"))
				_ = ud.Close()
			})
		})
	})

	Describe("TLS verification default", func() {
		// gofish reuses http.DefaultTransport's TLSClientConfig pointer; an
		// insecure connect elsewhere in the suite can flip InsecureSkipVerify on
		// that shared config and leak into this spec. Reset it so the assertion
		// reflects our VerifySSL handling, not cross-spec global state.
		It("fails against a self-signed BMC when VerifySSL is left on", func() {
			if tr, ok := http.DefaultTransport.(*http.Transport); ok && tr.TLSClientConfig != nil {
				tr.TLSClientConfig.InsecureSkipVerify = false
			}

			secure := redfish.NewDeployer(redfish.Config{
				Endpoint:  bmc.URL(),
				Username:  testUser,
				Password:  testPassword,
				VerifySSL: true, // explicit: verification stays on
				Timeout:   5 * time.Second,
			})
			err := secure.Connect(ctx)
			Expect(err).To(HaveOccurred(), "TLS verification must reject the self-signed cert by default")
			_ = secure.Close()
		})
	})
})
