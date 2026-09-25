package hook_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	cnst "github.com/kairos-io/kairos/v4/agent/pkg/constants"
	fsutils "github.com/kairos-io/kairos/v4/agent/pkg/utils/fs"
	"github.com/kairos-io/kairos/v4/sdk/collector"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
	"github.com/twpayne/go-vfs/v5"
	"github.com/twpayne/go-vfs/v5/vfst"

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
		var fs vfs.FS
		var cleanup func()
		var cfg *sdkConfig.Config

		BeforeEach(func() {
			var err error
			fs, cleanup, err = vfst.NewTestFS(nil)
			Expect(err).ToNot(HaveOccurred())
			cfg = config.NewConfig(config.WithFs(fs), config.WithLogger(sdkLogger.NewNullLogger()))
		})
		AfterEach(func() { cleanup() })

		// The hook mounts the persistent partition, and there is no partition
		// with that label here, so reaching the mount is what an error means
		// and succeeding is what returning early means. That is the only
		// handle on the hook's decision without a real disk.
		It("does nothing when nothing is declared and the media carries nothing", func() {
			cfg.Install = &sdkInstall.Install{}
			Expect(hook.ExtensionsPostInstall{}.Run(*cfg, nil)).To(Succeed())

			cfg.Install = nil
			Expect(hook.ExtensionsPostInstall{}.Run(*cfg, nil)).To(Succeed())
		})

		// Before this, the hook returned on `install.extensions` being empty,
		// so an extension dropped on an ISO was installed under UKI (by
		// SysExtPostInstall) and silently dropped on a GRUB install.
		It("runs for an extension on the live media with nothing declared", func() {
			Expect(fsutils.MkdirAll(fs, cnst.LiveDir, 0755)).To(Succeed())
			Expect(fs.WriteFile(filepath.Join(cnst.LiveDir, "gpg.sysext.raw"), []byte("image"), 0644)).To(Succeed())

			err := hook.ExtensionsPostInstall{}.Run(*cfg, nil)
			Expect(err).To(MatchError(ContainSubstring("mounting the persistent partition")))
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

	Describe("EnableExtensionsForBoot", func() {
		var fs vfs.FS
		var cleanup func()
		var cfg *sdkConfig.Config
		var dir string

		BeforeEach(func() {
			var err error
			fs, cleanup, err = vfst.NewTestFS(nil)
			Expect(err).ToNot(HaveOccurred())
			cfg = config.NewConfig(config.WithFs(fs), config.WithLogger(sdkLogger.NewNullLogger()))
			dir = "/var/lib/kairos/extensions"
			Expect(fsutils.MkdirAll(fs, dir, 0755)).To(Succeed())
			Expect(fs.WriteFile(filepath.Join(dir, "gpg.sysext.raw"), []byte("image"), 0644)).To(Succeed())
		})
		AfterEach(func() { cleanup() })

		// Staging the image is not enough: immucore reads only the per boot
		// state sub-directory when it populates /run/extensions, so without
		// the link systemd-sysext.service never even starts.
		It("links the extension into every boot state it installs for", func() {
			Expect(hook.EnableExtensionsForBoot(*cfg, dir, []string{"gpg.sysext.raw"})).To(Succeed())

			for _, bootState := range hook.BootStatesEnabledOnInstall {
				link := filepath.Join(dir, bootState, "gpg.sysext.raw")
				info, err := fs.Lstat(link)
				Expect(err).ToNot(HaveOccurred(), link)
				Expect(info.Mode()&os.ModeSymlink).ToNot(BeZero(), link)

				// The link has to be relative: this directory is written
				// through the persistent partition at /usr/local and read
				// back at /var/lib/kairos/extensions.
				target, err := fs.Readlink(link)
				Expect(err).ToNot(HaveOccurred())
				Expect(target).To(Equal("../gpg.sysext.raw"))

				content, err := fs.ReadFile(link)
				Expect(err).ToNot(HaveOccurred(), "the link does not resolve to the image")
				Expect(string(content)).To(Equal("image"))
			}
		})

		// It matches the UKI layout, where SysExtPostInstall writes into
		// active.efi.extra.d and passive.efi.extra.d and nowhere else.
		It("enables for active and passive, not recovery", func() {
			Expect(hook.BootStatesEnabledOnInstall).To(Equal([]string{"active", "passive"}))
		})

		// immucore only links .raw entries, so a link on anything else is one
		// nothing would ever follow.
		It("skips an entry that is not a raw image", func() {
			Expect(hook.EnableExtensionsForBoot(*cfg, dir, []string{"notes.txt"})).To(Succeed())
			_, err := fs.Lstat(filepath.Join(dir, "active", "notes.txt"))
			Expect(os.IsNotExist(err)).To(BeTrue())
		})

		It("is a no-op when nothing was installed", func() {
			Expect(hook.EnableExtensionsForBoot(*cfg, dir, nil)).To(Succeed())
			_, err := fs.Stat(filepath.Join(dir, "active"))
			Expect(os.IsNotExist(err)).To(BeTrue())
		})

		// Re-running the installer must not fail on the link it left behind.
		It("replaces a link that is already there", func() {
			Expect(hook.EnableExtensionsForBoot(*cfg, dir, []string{"gpg.sysext.raw"})).To(Succeed())
			Expect(hook.EnableExtensionsForBoot(*cfg, dir, []string{"gpg.sysext.raw"})).To(Succeed())
		})
	})

	Describe("LiveMediaExtensions", func() {
		var fs vfs.FS
		var cleanup func()
		var cfg *sdkConfig.Config

		BeforeEach(func() {
			var err error
			fs, cleanup, err = vfst.NewTestFS(nil)
			Expect(err).ToNot(HaveOccurred())
			cfg = config.NewConfig(config.WithFs(fs), config.WithLogger(sdkLogger.NewNullLogger()))
		})
		AfterEach(func() { cleanup() })

		// AuroraBoot lands an overlay_iso tree at the ISO root, which is
		// mounted here while the installer runs. This is how an airgapped
		// artifact ships an extension.
		It("finds the images on the live media", func() {
			Expect(fsutils.MkdirAll(fs, cnst.LiveDir, 0755)).To(Succeed())
			Expect(fs.WriteFile(filepath.Join(cnst.LiveDir, "gpg.sysext.raw"), []byte("image"), 0644)).To(Succeed())
			Expect(fs.WriteFile(filepath.Join(cnst.LiveDir, "config.yaml"), []byte("#cloud-config"), 0644)).To(Succeed())
			Expect(fs.WriteFile(filepath.Join(cnst.LiveDir, "tools.raw"), []byte("image"), 0644)).To(Succeed())

			Expect(hook.LiveMediaExtensions(*cfg)).To(Equal([]string{filepath.Join(cnst.LiveDir, "gpg.sysext.raw")}))
		})

		// An install that did not boot from removable media has no live
		// directory at all, and that is not a failure.
		It("reports nothing when there is no live media", func() {
			found, err := hook.LiveMediaExtensions(*cfg)
			Expect(err).ToNot(HaveOccurred())
			Expect(found).To(BeEmpty())
		})
	})

	Describe("StageLiveMediaExtensions", func() {
		var fs vfs.FS
		var cleanup func()
		var cfg *sdkConfig.Config
		var target string

		BeforeEach(func() {
			var err error
			fs, cleanup, err = vfst.NewTestFS(nil)
			Expect(err).ToNot(HaveOccurred())
			cfg = config.NewConfig(config.WithFs(fs), config.WithLogger(sdkLogger.NewNullLogger()))
			target = hook.PersistentExtensionsDir
			Expect(fsutils.MkdirAll(fs, target, 0755)).To(Succeed())
			Expect(fsutils.MkdirAll(fs, cnst.LiveDir, 0755)).To(Succeed())
			Expect(fs.WriteFile(filepath.Join(cnst.LiveDir, "gpg.sysext.raw"), []byte("image"), 0644)).To(Succeed())
		})
		AfterEach(func() { cleanup() })

		It("copies the image into the target and reports its name", func() {
			Expect(hook.StageLiveMediaExtensions(*cfg, nil, target)).To(Equal([]string{"gpg.sysext.raw"}))

			content, err := fs.ReadFile(filepath.Join(target, "gpg.sysext.raw"))
			Expect(err).ToNot(HaveOccurred())
			Expect(string(content)).To(Equal("image"))
		})

		// The declared entry resolved through a catalog or an OCI reference,
		// so it is the one to keep. Copying the media file over it would
		// replace a resolved image with whatever shares its name.
		It("leaves alone a name the config already installed", func() {
			staged, err := hook.StageLiveMediaExtensions(*cfg, []string{"gpg.sysext.raw"}, target)
			Expect(err).ToNot(HaveOccurred())
			Expect(staged).To(BeEmpty())

			_, err = fs.Stat(filepath.Join(target, "gpg.sysext.raw"))
			Expect(os.IsNotExist(err)).To(BeTrue())
		})

		It("copies into every target it is given", func() {
			second := "/efi/EFI/kairos/passive.efi.extra.d"
			Expect(fsutils.MkdirAll(fs, second, 0755)).To(Succeed())

			Expect(hook.StageLiveMediaExtensions(*cfg, nil, target, second)).To(Equal([]string{"gpg.sysext.raw"}))
			for _, dir := range []string{target, second} {
				_, err := fs.Stat(filepath.Join(dir, "gpg.sysext.raw"))
				Expect(err).ToNot(HaveOccurred(), dir)
			}
		})

		It("reports a copy it could not make when strict", func() {
			cfg.FailOnBundleErrors = true
			staged, err := hook.StageLiveMediaExtensions(*cfg, nil, "/not/there")
			Expect(err).To(HaveOccurred())
			Expect(staged).To(BeEmpty())
		})

		// The rest of the post-install hooks log and continue unless
		// FailOnBundleErrors is set, and a name that was not copied must not
		// be reported as staged: EnableExtensionsForBoot would link it and
		// leave a dangling symlink in the boot state directory.
		It("skips a copy it could not make when not strict", func() {
			staged, err := hook.StageLiveMediaExtensions(*cfg, nil, "/not/there")
			Expect(err).ToNot(HaveOccurred())
			Expect(staged).To(BeEmpty())
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
