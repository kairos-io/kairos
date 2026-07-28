package action

import (
	"os"
	"path/filepath"

	agentConfig "github.com/kairos-io/kairos-agent/v2/pkg/config"
	"github.com/kairos-io/kairos-sdk/collector"
	"github.com/kairos-io/kairos-sdk/state"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
)

var _ = Describe("RenderTemplate action test", func() {

	It("renders the template with config and state", func() {
		config := agentConfig.NewConfig()
		config.Collector = collector.Config{
			Values: collector.ConfigValues{
				"testKey": "testValue",
			},
		}
		runtime, err := state.NewRuntime()
		Expect(err).ToNot(HaveOccurred())

		result, err := RenderTemplate("../../tests/fixtures/template/test.yaml", config, runtime)
		Expect(err).ToNot(HaveOccurred())
		Expect(result).ToNot(BeNil())
		Expect(len(result)).ToNot(BeZero())

		var data map[string]string
		err = yaml.Unmarshal(result, &data)

		Expect(err).ToNot(HaveOccurred())
		Expect(data).To(HaveKeyWithValue("configTest", "TESTVALUE"))
		Expect(data["stateTest"]).To(Equal("amd64"))
	})

	It("fails when the template file does not exist", func() {
		config := agentConfig.NewConfig()
		config.Collector = collector.Config{}
		runtime, err := state.NewRuntime()
		Expect(err).ToNot(HaveOccurred())

		_, err = RenderTemplate("/non/existing/template.yaml", config, runtime)
		Expect(err).To(HaveOccurred())
	})

	It("fails when the template cannot be parsed", func() {
		config := agentConfig.NewConfig()
		config.Collector = collector.Config{}
		runtime, err := state.NewRuntime()
		Expect(err).ToNot(HaveOccurred())

		tmpl := filepath.Join(GinkgoT().TempDir(), "broken.yaml")
		Expect(os.WriteFile(tmpl, []byte("value: {{ .Config.unclosed"), 0644)).To(Succeed())

		_, err = RenderTemplate(tmpl, config, runtime)
		Expect(err).To(HaveOccurred())
	})

	It("fails when the template execution fails", func() {
		config := agentConfig.NewConfig()
		config.Collector = collector.Config{}
		runtime, err := state.NewRuntime()
		Expect(err).ToNot(HaveOccurred())

		tmpl := filepath.Join(GinkgoT().TempDir(), "failing.yaml")
		Expect(os.WriteFile(tmpl, []byte(`value: {{ fail "boom" }}`), 0644)).To(Succeed())

		_, err = RenderTemplate(tmpl, config, runtime)
		Expect(err).To(HaveOccurred())
	})
})
