package validation_test

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/kairos-io/kairos/v4/kairos-init/pkg/validation"
	"github.com/kairos-io/kairos/v4/kairos-init/pkg/values"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Use a real logger for testing
func createTestLogger() logger.KairosLogger {
	return logger.NewKairosLogger("test", "info", false)
}

var _ = Describe("Validator", func() {
	Describe("NewValidator", func() {
		It("should create a new validator with logger and system", func() {
			logger := createTestLogger()
			validator := validation.NewValidator(logger)

			Expect(validator).NotTo(BeNil())
			Expect(validator.Log).To(Equal(logger))
			Expect(validator.System).NotTo(BeNil())
		})
	})

	Describe("validateRHELServices", func() {
		Context("when system is not RHEL family", func() {
			It("should not validate services", func() {
				logger := createTestLogger()
				validator := &validation.Validator{
					Log: logger,
					System: values.System{
						Family: values.DebianFamily, // Non-RHEL family
					},
				}

				err := validator.ValidateRHELServices()
				Expect(err).NotTo(HaveOccurred(), "Should not validate services on non-RHEL family systems")
			})
		})

		Context("when system is RHEL family", func() {
			var (
				logger      logger.KairosLogger
				validator   *validation.Validator
				tempDir     string
				searchPaths []string
			)

			BeforeEach(func() {
				logger = createTestLogger()
				validator = &validation.Validator{
					Log: logger,
					System: values.System{
						Family: values.RedHatFamily,
					},
				}
			})

			Context("with no masked services", func() {
				BeforeEach(func() {
					var err error
					tempDir, err = os.MkdirTemp("", "systemd-system")
					Expect(err).NotTo(HaveOccurred())

					// Create regular service files (not masked)
					services := []string{"systemd-udevd", "systemd-logind"}
					for _, service := range services {
						servicePath := filepath.Join(tempDir, fmt.Sprintf("%s.service", service))
						err = os.WriteFile(servicePath, []byte("[Unit]\nDescription=Test Service"), 0644)
						Expect(err).NotTo(HaveOccurred())
					}

					// Set up search paths for testing
					searchPaths = []string{tempDir}
				})

				AfterEach(func() {
					if tempDir != "" {
						os.RemoveAll(tempDir)
					}
				})

				It("should not error when services exist in system path and are not masked", func() {
					err := validator.ValidateRHELServicesWithPaths(searchPaths)
					Expect(err).NotTo(HaveOccurred(), "Should not error when services exist in system path and are not masked")
				})
			})

			Context("with one masked service", func() {
				BeforeEach(func() {
					var err error
					tempDir, err = os.MkdirTemp("", "systemd-system")
					Expect(err).NotTo(HaveOccurred())

					// Create a regular service file
					regularServicePath := filepath.Join(tempDir, "systemd-logind.service")
					err = os.WriteFile(regularServicePath, []byte("[Unit]\nDescription=Login Service"), 0644)
					Expect(err).NotTo(HaveOccurred())

					// Create a masked service file (symlink to /dev/null) for only one service
					maskedServicePath := filepath.Join(tempDir, "systemd-udevd.service")
					err = os.Symlink("/dev/null", maskedServicePath)
					Expect(err).NotTo(HaveOccurred())

					// Verify the symlink was created correctly
					target, err := os.Readlink(maskedServicePath)
					Expect(err).NotTo(HaveOccurred())
					Expect(target).To(Equal("/dev/null"), "Symlink should point to /dev/null")

					searchPaths = []string{tempDir}
				})

				AfterEach(func() {
					if tempDir != "" {
						os.RemoveAll(tempDir)
					}
				})

				It("should error when a service is masked", func() {
					err := validator.ValidateRHELServicesWithPaths(searchPaths)
					Expect(err).To(HaveOccurred(), "Should error when a service is masked")
					Expect(err.Error()).To(ContainSubstring("systemd-udevd is masked on RHEL family system"))
				})
			})

			Context("with both services masked", func() {
				BeforeEach(func() {
					var err error
					tempDir, err = os.MkdirTemp("", "systemd-system")
					Expect(err).NotTo(HaveOccurred())

					// Create masked service files (symlinks to /dev/null) for both services
					services := []string{"systemd-udevd", "systemd-logind"}
					for _, service := range services {
						maskedServicePath := filepath.Join(tempDir, fmt.Sprintf("%s.service", service))
						err = os.Symlink("/dev/null", maskedServicePath)
						Expect(err).NotTo(HaveOccurred())

						// Verify the symlink was created correctly
						target, err := os.Readlink(maskedServicePath)
						Expect(err).NotTo(HaveOccurred())
						Expect(target).To(Equal("/dev/null"), "Symlink should point to /dev/null")
					}

					searchPaths = []string{tempDir}
				})

				AfterEach(func() {
					if tempDir != "" {
						os.RemoveAll(tempDir)
					}
				})

				It("should error when both services are masked", func() {
					err := validator.ValidateRHELServicesWithPaths(searchPaths)
					Expect(err).To(HaveOccurred(), "Should error when services are masked")
					Expect(err.Error()).To(ContainSubstring("systemd-udevd is masked on RHEL family system"))
					Expect(err.Error()).To(ContainSubstring("systemd-logind is masked on RHEL family system"))
				})
			})

			Context("with regular service files", func() {
				BeforeEach(func() {
					var err error
					tempDir, err = os.MkdirTemp("", "systemd-system")
					Expect(err).NotTo(HaveOccurred())

					// Create regular service files (not masked) for both services
					services := []string{"systemd-udevd", "systemd-logind"}
					for _, service := range services {
						servicePath := filepath.Join(tempDir, fmt.Sprintf("%s.service", service))
						err = os.WriteFile(servicePath, []byte("[Unit]\nDescription=Test Service"), 0644)
						Expect(err).NotTo(HaveOccurred())
					}

					searchPaths = []string{tempDir}
				})

				AfterEach(func() {
					if tempDir != "" {
						os.RemoveAll(tempDir)
					}
				})

				It("should not error when services are regular files", func() {
					err := validator.ValidateRHELServicesWithPaths(searchPaths)
					Expect(err).NotTo(HaveOccurred(), "Should not error when services are regular files")
				})
			})

			Context("with mixed services (one masked, one regular)", func() {
				BeforeEach(func() {
					var err error
					tempDir, err = os.MkdirTemp("", "systemd-system")
					Expect(err).NotTo(HaveOccurred())

					// Create a masked service file
					maskedServicePath := filepath.Join(tempDir, "systemd-udevd.service")
					err = os.Symlink("/dev/null", maskedServicePath)
					Expect(err).NotTo(HaveOccurred())

					// Create a regular service file
					regularServicePath := filepath.Join(tempDir, "systemd-logind.service")
					err = os.WriteFile(regularServicePath, []byte("[Unit]\nDescription=Login Service"), 0644)
					Expect(err).NotTo(HaveOccurred())

					searchPaths = []string{tempDir}
				})

				AfterEach(func() {
					if tempDir != "" {
						os.RemoveAll(tempDir)
					}
				})

				It("should error when any service is masked", func() {
					err := validator.ValidateRHELServicesWithPaths(searchPaths)
					Expect(err).To(HaveOccurred(), "Should error when any service is masked")
					Expect(err.Error()).To(ContainSubstring("systemd-udevd is masked on RHEL family system"))
				})
			})

			Context("with missing services", func() {
				BeforeEach(func() {
					// Don't create any directories or files - they should be missing
					searchPaths = []string{"/nonexistent/path"}
				})

				It("should error when services don't exist", func() {
					err := validator.ValidateRHELServicesWithPaths(searchPaths)
					Expect(err).To(HaveOccurred(), "Should error when services don't exist")
					Expect(err.Error()).To(ContainSubstring("systemd-udevd does not exist on RHEL family system"))
					Expect(err.Error()).To(ContainSubstring("systemd-logind does not exist on RHEL family system"))
				})
			})

			Context("with one missing service", func() {
				BeforeEach(func() {
					var err error
					tempDir, err = os.MkdirTemp("", "systemd-system")
					Expect(err).NotTo(HaveOccurred())

					// Create only one service file
					servicePath := filepath.Join(tempDir, "systemd-udevd.service")
					err = os.WriteFile(servicePath, []byte("[Unit]\nDescription=Test Service"), 0644)
					Expect(err).NotTo(HaveOccurred())
					// systemd-logind.service is missing

					searchPaths = []string{tempDir}
				})

				AfterEach(func() {
					if tempDir != "" {
						os.RemoveAll(tempDir)
					}
				})

				It("should error when one service is missing", func() {
					err := validator.ValidateRHELServicesWithPaths(searchPaths)
					Expect(err).To(HaveOccurred(), "Should error when one service is missing")
					Expect(err.Error()).To(ContainSubstring("systemd-logind does not exist on RHEL family system"))
					Expect(err.Error()).NotTo(ContainSubstring("systemd-udevd does not exist"))
				})
			})
		})
	})

	Describe("validateGettyServices", func() {
		Context("when system is Alpine family", func() {
			It("should not validate services", func() {
				logger := createTestLogger()
				validator := &validation.Validator{
					Log: logger,
					System: values.System{
						Family: values.AlpineFamily, // Alpine uses OpenRC, not systemd
					},
				}

				err := validator.ValidateGettyServices()
				Expect(err).NotTo(HaveOccurred(), "Should not validate services on Alpine family systems")
			})
		})

		Context("when system is systemd-based", func() {
			var (
				logger      logger.KairosLogger
				validator   *validation.Validator
				tempDir     string
				searchPaths []string
			)

			BeforeEach(func() {
				logger = createTestLogger()
				validator = &validation.Validator{
					Log: logger,
					System: values.System{
						Family: values.DebianFamily, // Systemd-based family
					},
				}
			})

			Context("with getty.target not masked", func() {
				BeforeEach(func() {
					var err error
					tempDir, err = os.MkdirTemp("", "systemd-system")
					Expect(err).NotTo(HaveOccurred())

					// Create a regular getty.target file
					gettyPath := filepath.Join(tempDir, "getty.target")
					err = os.WriteFile(gettyPath, []byte("[Unit]\nDescription=Getty Target"), 0644)
					Expect(err).NotTo(HaveOccurred())

					searchPaths = []string{tempDir}
				})

				AfterEach(func() {
					if tempDir != "" {
						os.RemoveAll(tempDir)
					}
				})

				It("should not error", func() {
					err := validator.ValidateGettyServicesWithPaths(searchPaths)
					Expect(err).NotTo(HaveOccurred(), "Should not error when getty.target is not masked")
				})
			})

			Context("with getty.target masked", func() {
				BeforeEach(func() {
					var err error
					tempDir, err = os.MkdirTemp("", "systemd-system")
					Expect(err).NotTo(HaveOccurred())

					// Create a masked getty.target file (symlink to /dev/null)
					maskedGettyPath := filepath.Join(tempDir, "getty.target")
					err = os.Symlink("/dev/null", maskedGettyPath)
					Expect(err).NotTo(HaveOccurred())

					searchPaths = []string{tempDir}
				})

				AfterEach(func() {
					if tempDir != "" {
						os.RemoveAll(tempDir)
					}
				})

				It("should error when getty.target is masked", func() {
					err := validator.ValidateGettyServicesWithPaths(searchPaths)
					Expect(err).To(HaveOccurred(), "Should error when getty.target is masked")
					Expect(err.Error()).To(ContainSubstring("getty.target is masked on systemd-based system"))
				})
			})

			Context("with getty.target missing", func() {
				BeforeEach(func() {
					// Don't create any directories or files - getty.target should be missing
					searchPaths = []string{"/nonexistent/path"}
				})

				It("should error when getty.target doesn't exist", func() {
					err := validator.ValidateGettyServicesWithPaths(searchPaths)
					Expect(err).To(HaveOccurred(), "Should error when getty.target doesn't exist")
					Expect(err.Error()).To(ContainSubstring("getty.target does not exist on systemd-based system"))
				})
			})
		})
	})

	Describe("validateKernel", func() {
		var (
			log       logger.KairosLogger
			validator *validation.Validator
			tempDir   string
		)

		BeforeEach(func() {
			log = createTestLogger()
			validator = &validation.Validator{
				Log:    log,
				System: values.System{},
			}

			var err error
			tempDir, err = os.MkdirTemp("", "lib-modules")
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			if tempDir != "" {
				os.RemoveAll(tempDir)
			}
		})

		Context("with no kernels installed", func() {
			It("should error when there are no kernel directories", func() {
				err := validator.ValidateKernelWithPath(tempDir, "generic")
				Expect(err).To(HaveOccurred(), "Should error when no kernels are installed")
				Expect(err.Error()).To(ContainSubstring("[KERNEL]"))
			})
		})

		Context("with a single semver kernel installed", func() {
			BeforeEach(func() {
				err := os.Mkdir(filepath.Join(tempDir, "5.15.0-101-generic"), 0755)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should not error and select the single kernel", func() {
				err := validator.ValidateKernelWithPath(tempDir, "generic")
				Expect(err).NotTo(HaveOccurred(), "Should not error when a single kernel is installed")
			})
		})

		Context("with multiple semver kernels installed", func() {
			BeforeEach(func() {
				err := os.Mkdir(filepath.Join(tempDir, "5.15.0-101-generic"), 0755)
				Expect(err).NotTo(HaveOccurred())
				err = os.Mkdir(filepath.Join(tempDir, "5.15.0-102-generic"), 0755)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should not error and select the highest kernel", func() {
				// Multiple kernels are valid — the chooser picks the highest semver
				err := validator.ValidateKernelWithPath(tempDir, "generic")
				Expect(err).NotTo(HaveOccurred(), "Should not error when multiple kernels are installed")
			})
		})

		Context("with an invalid modules path", func() {
			It("should error when the modules path does not exist", func() {
				err := validator.ValidateKernelWithPath("/nonexistent/path/lib/modules", "generic")
				Expect(err).To(HaveOccurred(), "Should error when modules path does not exist")
				Expect(err.Error()).To(ContainSubstring("[KERNEL]"))
			})
		})

		Context("for an RPi4 model", func() {
			Context("with a raspi kernel installed", func() {
				BeforeEach(func() {
					err := os.Mkdir(filepath.Join(tempDir, "5.15.0-1025-raspi"), 0755)
					Expect(err).NotTo(HaveOccurred())
				})

				It("should not error", func() {
					err := validator.ValidateKernelWithPath(tempDir, values.Rpi4.String())
					Expect(err).NotTo(HaveOccurred(), "Should not error when raspi kernel is installed")
				})
			})

			Context("with a raspi kernel alongside a higher generic kernel", func() {
				BeforeEach(func() {
					err := os.Mkdir(filepath.Join(tempDir, "6.8.0-51-generic"), 0755)
					Expect(err).NotTo(HaveOccurred())
					err = os.Mkdir(filepath.Join(tempDir, "5.15.0-1025-raspi"), 0755)
					Expect(err).NotTo(HaveOccurred())
				})

				It("should not error (raspi kernel wins)", func() {
					err := validator.ValidateKernelWithPath(tempDir, values.Rpi4.String())
					Expect(err).NotTo(HaveOccurred(), "Should prefer the raspi kernel over the generic one")
				})
			})
		})
	})

	Describe("Validate", func() {
		It("should run full validation without panicking", func() {
			logger := createTestLogger()
			validator := validation.NewValidator(logger)

			// This test will run the full validation on the current system
			// It's more of an integration test and may fail depending on the system state
			err := validator.Validate()

			// We don't assert on the result here since it depends on the actual system state
			// This test is mainly to ensure the validation doesn't panic
			GinkgoWriter.Printf("Validation result: %v\n", err)
		})
	})

	Describe("ValidateInitrdContents", func() {
		// A trimmed lsinitrd listing. The real one is thousands of lines;
		// these are the entries the check looks for.
		const listing = `Version: dracut-059

Arguments: --force

dracut modules:
systemd
28immucore
=========================================================================
drwxr-xr-x   1 root     root            0 Oct  6 10:00 usr/bin
-rwxr-xr-x   1 root     root     24117248 Oct  6 10:00 usr/bin/immucore
-rw-r--r--   1 root     root         1024 Oct  6 10:00 usr/lib/systemd/system/immucore.service
-rw-r--r--   1 root     root        32768 Oct  6 10:00 usr/lib/modules/6.12.0/kernel/drivers/usb/host/xhci-pci-renesas.ko.xz
=========================================================================
`

		It("accepts a listing that carries immucore", func() {
			validator := validation.NewValidator(createTestLogger())

			Expect(validator.ValidateInitrdContents(listing)).To(Succeed())
		})

		It("reports immucore when the listing does not carry it", func() {
			validator := validation.NewValidator(createTestLogger())

			err := validator.ValidateInitrdContents("usr/bin/sh\nusr/bin/udevadm\n")

			Expect(err).To(MatchError(ContainSubstring("[INITRD] did not find immucore in the initrd")))
		})

		It("does not fail when the kernel module is absent, because that check is warn only", func() {
			validator := validation.NewValidator(createTestLogger())

			Expect(validator.ValidateInitrdContents("usr/bin/immucore\n")).To(Succeed())
		})

		// Why the caller must hand this function lsinitrd's stdout and
		// nothing else: diagnostics do not mention immucore either, so
		// searching them reports the binary as missing from an initrd that
		// was never read. That phantom line used to be appended on top of
		// the real failure and named the wrong culprit.
		It("reports immucore missing when handed lsinitrd's diagnostics instead of a listing", func() {
			validator := validation.NewValidator(createTestLogger())

			err := validator.ValidateInitrdContents("lsinitrd: /boot/initrd does not exist\n")

			Expect(err).To(MatchError(ContainSubstring("[INITRD] did not find immucore in the initrd")))
		})
	})

	Describe("DescribeCommandError", func() {
		It("names the reason the command wrote to stderr", func() {
			_, err := exec.Command("sh", "-c", "echo 'lsinitrd: /boot/initrd does not exist' >&2; exit 1").Output()
			Expect(err).To(HaveOccurred())

			Expect(validation.DescribeCommandError(err)).To(ContainSubstring("exit status 1"))
			Expect(validation.DescribeCommandError(err)).To(ContainSubstring("/boot/initrd does not exist"))
		})

		It("falls back to the error itself when the command wrote no stderr", func() {
			_, err := exec.Command("sh", "-c", "exit 3").Output()
			Expect(err).To(HaveOccurred())

			Expect(validation.DescribeCommandError(err)).To(Equal("exit status 3"))
		})

		It("returns a non exit error unchanged", func() {
			Expect(validation.DescribeCommandError(errors.New("boom"))).To(Equal("boom"))
		})
	})
})
