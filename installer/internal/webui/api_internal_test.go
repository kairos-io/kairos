package webui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"

	"github.com/kairos-io/kairos/v4/sdk/branding"
	sdkBus "github.com/kairos-io/kairos/v4/sdk/bus"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/kairos/v4/installer/internal/disks"
	"github.com/kairos-io/kairos/v4/installer/internal/wizard"
)

type webEnv struct{}

func (webEnv) Disks() ([]disks.Disk, error) {
	return []disks.Disk{{Path: "/dev/vda", Size: "40.00 GiB", Model: "QEMU HARDDISK"}}, nil
}
func (webEnv) Extensions(context.Context) ([]wizard.Choice, error) {
	return []wizard.Choice{{Value: "tailscale", Label: "tailscale"}}, nil
}
func (webEnv) Timezones() []string                  { return []string{"UTC", "Europe/Rome"} }
func (webEnv) Keymaps() []string                    { return []string{"it"} }
func (webEnv) ProviderPrompts() []sdkBus.YAMLPrompt { return nil }
func (webEnv) AdvancedDisabled() bool               { return false }

// brandedEnv is an image with the advanced customization switched off.
type brandedEnv struct{ webEnv }

func (brandedEnv) AdvancedDisabled() bool { return true }

// providerEnv adds the prompts provider-kairos sends.
type providerEnv struct{ webEnv }

func (providerEnv) ProviderPrompts() []sdkBus.YAMLPrompt {
	return []sdkBus.YAMLPrompt{
		{YAMLSection: "p2p.network_token", Prompt: "Insert a network token, leave empty to autogenerate",
			AskFirst: true, AskPrompt: "Do you want to setup a full mesh-support?", IfEmpty: "generated-token"},
		{YAMLSection: "k3s.enabled", Bool: true, Prompt: "Do you want to enable k3s?"},
	}
}

var _ = Describe("the wizard API", func() {
	var srv *httptest.Server
	BeforeEach(func() {
		srv = httptest.NewServer(newServer(Options{Env: webEnv{}, Source: "oci:boot"}))
		DeferCleanup(srv.Close)
	})

	post := func(path string, body any, out any) *http.Response {
		b, _ := json.Marshal(body)
		resp, err := http.Post(srv.URL+path, "application/json", bytes.NewReader(b))
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(resp.Body.Close)
		if out != nil {
			Expect(json.NewDecoder(resp.Body).Decode(out)).To(Succeed())
		}
		return resp
	}

	postInstallJSON := func(body map[string]string) *http.Response {
		client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		b, _ := json.Marshal(body)
		resp, err := client.Post(srv.URL+"/install", "application/json", bytes.NewReader(b))
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(resp.Body.Close)
		return resp
	}

	It("serves the steps", func() {
		resp, err := http.Get(srv.URL + "/api/wizard")
		Expect(err).ToNot(HaveOccurred())
		defer resp.Body.Close()
		var got struct{ Steps []wizard.Step }
		Expect(json.NewDecoder(resp.Body).Decode(&got)).To(Succeed())
		Expect(got.Steps[0].ID).To(Equal(wizard.StepDisk))
		Expect(got.Steps[0].Fields[0].Choices[0].Value).To(Equal("/dev/vda"))
	})

	It("says whether the branding switch turned the advanced customization off", func() {
		for _, tc := range []struct {
			env  wizard.Env
			want bool
		}{{webEnv{}, false}, {brandedEnv{}, true}} {
			s := httptest.NewServer(newServer(Options{Env: tc.env}))
			resp, err := http.Get(s.URL + "/api/wizard")
			Expect(err).ToNot(HaveOccurred())
			var raw map[string]json.RawMessage
			Expect(json.NewDecoder(resp.Body).Decode(&raw)).To(Succeed())
			resp.Body.Close()
			s.Close()
			Expect(raw).To(HaveKey("advanced_disabled"))
			Expect(string(raw["advanced_disabled"])).To(Equal(fmt.Sprint(tc.want)))
		}
	})

	It("applies a step and returns the answers, or the field errors", func() {
		var ok struct {
			Answers wizard.Answers
			Errors  []wizard.FieldError
		}
		post("/api/step/"+wizard.StepHostname, map[string]any{"answers": wizard.Answers{Disk: "/dev/vda"}, "values": map[string]string{"hostname": "edge-01"}}, &ok)
		Expect(ok.Errors).To(BeEmpty())
		Expect(ok.Answers.Hostname).To(Equal("edge-01"))
		Expect(ok.Answers.Disk).To(Equal("/dev/vda"))

		var bad struct{ Errors []wizard.FieldError }
		post("/api/step/"+wizard.StepHostname, map[string]any{"values": map[string]string{"hostname": "-x"}}, &bad)
		Expect(bad.Errors).To(HaveLen(1))
		Expect(bad.Errors[0].Field).To(Equal("hostname"))
	})

	It("sends an empty error list, not null, on success", func() {
		var raw map[string]json.RawMessage
		post("/api/step/"+wizard.StepHostname, map[string]any{"values": map[string]string{"hostname": "edge-01"}}, &raw)
		Expect(string(raw["errors"])).To(Equal("[]"))
	})

	It("hashes the password on the server and never echoes it", func() {
		var got struct{ Answers wizard.Answers }
		resp := post("/api/step/"+wizard.StepUser, map[string]any{"values": map[string]string{"username": "kairos", "password": "s3cret", "password_confirm": "s3cret"}}, &got)
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		Expect(got.Answers.PasswordHash).To(HavePrefix("$6$"))
		Expect(got.Answers.PasswordHash).ToNot(ContainSubstring("s3cret"))
	})

	It("renders with the installer's source, never one from the browser", func() {
		var got struct {
			CloudConfig string `json:"cloud_config"`
		}
		post("/api/render", map[string]any{"answers": map[string]any{"disk": "/dev/vda", "source": "oci:evil"}}, &got)
		Expect(got.CloudConfig).To(ContainSubstring("source: oci:boot"))
		Expect(got.CloudConfig).ToNot(ContainSubstring("oci:evil"))
	})

	It("refuses to render a timezone that would reach the shell, with a 422", func() {
		var got map[string]string
		resp := post("/api/render", map[string]any{"answers": map[string]any{"disk": "/dev/vda", "timezone": "UTC; reboot"}}, &got)
		Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
		Expect(got["error"]).To(ContainSubstring("timezone"))
		Expect(got).ToNot(HaveKey("cloud_config"))
	})

	It("validates a cloud-config as JSON, an empty error meaning valid", func() {
		var ok, bad struct{ Error string }
		post("/validate-json", map[string]string{"cloud_config": "#cloud-config\nusers:\n  - name: kairos\n    passwd: kairos\n"}, &ok)
		Expect(ok.Error).To(BeEmpty())
		resp := post("/validate-json", map[string]string{"cloud_config": "#cloud-config\ninstall: {device: 7}\n"}, &bad)
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		Expect(bad.Error).ToNot(BeEmpty())
	})

	It("answers a malformed body with a 400", func() {
		resp, err := http.Post(srv.URL+"/api/render", "application/json", strings.NewReader("{not json"))
		Expect(err).ToNot(HaveOccurred())
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
	})

	It("installs an edited cloud-config with the confirmed device written back", func() {
		GinkgoT().Setenv("KAIROS_AGENT_BIN", stubAgent(`
for a in "$@"; do cfg="$a"; done
cp "$cfg" "$TESTCFG"
echo '{"event":"step","step":"done"}'
`))
		cfgOut := GinkgoT().TempDir() + "/cfg.yaml"
		GinkgoT().Setenv("TESTCFG", cfgOut)
		resp := postInstallJSON(map[string]string{"cloud_config": "#cloud-config\ninstall:\n  device: /dev/vdb\n", "device": "/dev/vda", "finish_action": "reboot"})
		Expect(resp.StatusCode).To(Equal(http.StatusSeeOther))
		Eventually(func() string { b, _ := os.ReadFile(cfgOut); return string(b) }, "5s").Should(And(
			ContainSubstring("device: /dev/vda"), ContainSubstring("reboot: true")))
	})

	It("refuses a finish action it does not know, and starts nothing", func() {
		started := GinkgoT().TempDir() + "/started"
		GinkgoT().Setenv("KAIROS_AGENT_BIN", stubAgent(`echo run >> `+started+`
echo '{"event":"step","step":"done"}'
`))
		resp := postInstallJSON(map[string]string{"cloud_config": "#cloud-config\n", "device": "/dev/vda", "finish_action": "halt"})
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		body := new(bytes.Buffer)
		_, _ = body.ReadFrom(resp.Body)
		Expect(body.String()).To(ContainSubstring("halt"))
		Consistently(func() bool { _, err := os.Stat(started); return err == nil }, "500ms").Should(BeFalse())
	})

	It("writes no provider section when the provider step is submitted with its defaults", func() {
		psrv := httptest.NewServer(newServer(Options{Env: providerEnv{}}))
		DeferCleanup(psrv.Close)
		b, _ := json.Marshal(map[string]any{"answers": map[string]any{}, "values": map[string]string{
			"p2p.network_token" + wizard.AskSuffix: "false", "p2p.network_token": "", "k3s.enabled": "false",
		}})
		resp, err := http.Post(psrv.URL+"/api/step/"+wizard.StepProvider, "application/json", bytes.NewReader(b))
		Expect(err).ToNot(HaveOccurred())
		defer resp.Body.Close()
		var raw struct{ Answers map[string]json.RawMessage }
		Expect(json.NewDecoder(resp.Body).Decode(&raw)).To(Succeed())
		Expect(raw.Answers).ToNot(HaveKey("provider"))
	})

	Describe("behind the token", func() {
		It("refuses /api/wizard without it and serves it with it", func() {
			guarded := httptest.NewServer(newServer(Options{Env: webEnv{}, WebUI: branding.WebUI{Token: "tok"}}))
			DeferCleanup(guarded.Close)

			resp := get("/api/wizard")(noRedirect(), guarded.URL, nil)
			Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))

			resp = get("/api/wizard")(noRedirect(), guarded.URL, bearer("tok"))
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})
	})
})
