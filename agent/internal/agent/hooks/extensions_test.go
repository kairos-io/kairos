package hook_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kairos-io/kairos/v4/sdk/collector"

	hook "github.com/kairos-io/kairos/v4/agent/internal/agent/hooks"
	"github.com/kairos-io/kairos/v4/agent/pkg/config"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	extensiontypes "github.com/kairos-io/kairos/v4/sdk/types/extensions"
	sdkInstall "github.com/kairos-io/kairos/v4/sdk/types/install"
	"github.com/mudler/yip/pkg/schema"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Extension hooks", func() {
	Describe("ExtensionsPostInstall", func() {
		// The hook mounts the persistent partition, so the only thing worth
		// asserting without a real disk is that it does not get that far when
		// there is nothing to install.
		It("does nothing when no extension is declared", func() {
			cfg := &sdkConfig.Config{Install: &sdkInstall.Install{}}
			Expect(hook.ExtensionsPostInstall{}.Run(*cfg, nil)).To(Succeed())

			cfg = &sdkConfig.Config{}
			Expect(hook.ExtensionsPostInstall{}.Run(*cfg, nil)).To(Succeed())
		})
	})

	Describe("ExtensionSignaturePolicy", func() {
		It("does nothing unless ignore_signatures is set", func() {
			cfg := config.NewConfig()
			Expect(hook.ExtensionSignaturePolicy{}.Run(*cfg, nil)).To(Succeed())
		})

		Describe("the cloud config it installs", func() {
			var stage schema.Stage

			BeforeEach(func() {
				boot := hook.IgnoreSignaturesCloudConfig().Stages["boot"]
				Expect(boot).To(HaveLen(1))
				stage = boot[0]
			})

			It("drops an override into both extension units", func() {
				var paths []string
				for _, file := range stage.Files {
					paths = append(paths, file.Path)
				}
				sort.Strings(paths)
				Expect(paths).To(Equal([]string{
					"/etc/systemd/system/systemd-confext.service.d/zz-kairos-ignore-signatures.conf",
					"/etc/systemd/system/systemd-sysext.service.d/zz-kairos-ignore-signatures.conf",
				}))
			})

			// systemd applies drop-ins in lexical order and the last
			// ExecStart wins, so this one has to sort after the drop-ins
			// kairos-init ships or the strict policy stays in force.
			It("sorts after the drop-ins kairos-init ships", func() {
				for _, file := range stage.Files {
					name := file.Path[strings.LastIndex(file.Path, "/")+1:]
					Expect(name > "kairos.conf").To(BeTrue(), name)
					Expect(name > "kairos-uki.conf").To(BeTrue(), name)
				}
			})

			It("relaxes the image policy to accept an unprotected image", func() {
				for _, file := range stage.Files {
					unit := "systemd-sysext"
					if strings.Contains(file.Path, "confext") {
						unit = "systemd-confext"
					}
					// Both ExecStart and ExecReload have to be reset first,
					// otherwise the override is appended to the strict one and
					// the unit runs two refreshes.
					Expect(file.Content).To(ContainSubstring("\nExecStart=\nExecStart=" + unit + " refresh --image-policy="))
					Expect(file.Content).To(ContainSubstring("\nExecReload=\nExecReload=" + unit + " refresh --image-policy="))
					Expect(file.Content).To(ContainSubstring("unprotected"))
					Expect(file.Content).ToNot(ContainSubstring("root=signed+absent"))
					Expect(file.Content).ToNot(ContainSubstring("root=verity+absent"))
				}
			})

			// The units have already started with the strict policy by the
			// time the boot stage runs, so the setting has to take effect
			// without a second reboot.
			It("reloads and re-merges so it applies on this boot", func() {
				Expect(stage.Commands).To(ContainElement("systemctl daemon-reload"))
				Expect(strings.Join(stage.Commands, "\n")).To(ContainSubstring("systemctl restart systemd-sysext systemd-confext"))
			})
		})
	})

	Describe("PersistentExtensionsDir", func() {
		// immucore derives the bind source from the runtime path by replacing
		// the separators and appending .bind. If either side changes, the
		// extensions staged at install time land somewhere nothing reads.
		It("matches the bind source immucore mounts over /var/lib/kairos", func() {
			Expect(hook.PersistentExtensionsDir).To(Equal("/usr/local/.state/var-lib-kairos.bind/extensions"))
		})
	})
})

var _ = Describe("Extension config plumbing", func() {
	// The config types are only useful if a cloud config actually reaches
	// them, so read one the way the agent does.
	It("reaches the agent config from a cloud config on disk", func() {
		directory := GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(directory, "extensions.yaml"), []byte(`#cloud-config
extensions:
  catalogs:
    - https://example.org/one.json
    - https://example.org/two.json
  ignore_signatures: true
install:
  extensions:
    - fwupd@2.1.7
    - name: git
      version: ">= 2.50, < 3"
    - oci://ghcr.io/example/tools.sysext.raw
`), 0644)).To(Succeed())

		cfg, err := config.ScanNoLogs(collector.Directories(directory), collector.NoLogs)
		Expect(err).ToNot(HaveOccurred())

		Expect(cfg.Extensions.Catalogs).To(Equal([]string{"https://example.org/one.json", "https://example.org/two.json"}))
		Expect(cfg.Extensions.IgnoreSignatures).To(BeTrue())
		Expect(cfg.Extensions.CatalogURLs()).To(Equal(cfg.Extensions.Catalogs))
		Expect(cfg.Install).ToNot(BeNil())
		Expect(cfg.Install.Extensions).To(Equal(extensiontypes.Extensions{
			{Name: "fwupd", Version: "2.1.7"},
			{Name: "git", Version: ">= 2.50, < 3"},
			{Name: "oci://ghcr.io/example/tools.sysext.raw"},
		}))
	})

	It("defaults the catalog list when the cloud config names none", func() {
		Expect(extensiontypes.Config{}.CatalogURLs()).To(Equal([]string{extensiontypes.DefaultCatalogURL}))
	})
})
