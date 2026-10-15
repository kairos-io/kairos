package clusterplugin

import (
	"encoding/json"

	"github.com/kairos-io/kairos/v4/sdk/bus"
	"github.com/mudler/go-pluggable"
	yip "github.com/mudler/yip/pkg/schema"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v4/vfst"
	"gopkg.in/yaml.v3"
)

var _ = Describe("ClusterPlugin.onBoot", func() {
	var plugin ClusterPlugin
	var cleanup func()

	BeforeEach(func() {
		var testFS *vfst.TestFS
		var err error
		testFS, cleanup, err = vfst.NewTestFS(map[string]interface{}{})
		Expect(err).ToNot(HaveOccurred())
		filesystem = testFS

		plugin = ClusterPlugin{
			Provider: func(cluster Cluster) yip.YipConfig {
				return yip.YipConfig{Name: "test"}
			},
		}
	})

	AfterEach(func() {
		cleanup()
	})

	newBootEvent := func(cluster *Cluster) *pluggable.Event {
		cfg := Config{Cluster: cluster}
		cfgYAML, err := yaml.Marshal(cfg)
		Expect(err).ToNot(HaveOccurred())

		payload, err := json.Marshal(bus.EventPayload{Config: string(cfgYAML)})
		Expect(err).ToNot(HaveOccurred())

		return &pluggable.Event{Name: bus.EventBoot, Data: string(payload)}
	}

	// On a UKI install or reset, onBoot runs before anything else has created
	// /usr/local/cloud-config: it normally comes into being as a side effect of
	// the first RunStage call, which only happens post-pivot. Without creating
	// the directory here, OpenFile fails with ENOENT.
	It("creates the cloud-config directory when it does not exist yet", func() {
		response := plugin.onBoot(newBootEvent(&Cluster{Role: RoleControlPlane}))
		Expect(response.Errored()).To(BeFalse(), response.Error)

		content, err := filesystem.ReadFile(clusterProviderCloudConfigFile)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(content)).To(ContainSubstring("#cloud-config"))
	})

	It("creates the parent directory of a custom ClusterConfigPath", func() {
		customPath := "/oem/custom/cluster-config.yaml"
		response := plugin.onBoot(newBootEvent(&Cluster{Role: RoleControlPlane, ClusterConfigPath: customPath}))
		Expect(response.Errored()).To(BeFalse(), response.Error)

		content, err := filesystem.ReadFile(customPath)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(content)).To(ContainSubstring("#cloud-config"))
	})

	It("is a no-op when config.Cluster is nil", func() {
		response := plugin.onBoot(newBootEvent(nil))
		Expect(response.Errored()).To(BeFalse(), response.Error)

		_, err := filesystem.ReadFile(clusterProviderCloudConfigFile)
		Expect(err).To(HaveOccurred())
	})
})
