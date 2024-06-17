// nolint
package mos_test

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lmittmann/ppm"
	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/spectrocloud/peg/matcher"
)

var _ = Describe("kairos qr code install", Label("provider", "provider-qrcode-install"), func() {
	var vm VM

	BeforeEach(func() {
		_, vm = startVM()
		vm.EventuallyConnects(1200)
	})

	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			gatherLogs(vm)
			serial, _ := os.ReadFile(filepath.Join(vm.StateDir, "serial.log"))
			_ = os.MkdirAll("logs", os.ModePerm|os.ModeDir)
			_ = os.WriteFile(filepath.Join("logs", "serial.log"), serial, os.ModePerm)
			fmt.Println(string(serial))
		}
		vm.Destroy(nil)
	})

	It("installs to disk with custom config", func() {
		By("checking if is has default service active")
		if isFlavor(vm, "alpine") {
			out, _ := vm.Sudo("rc-status")
			Expect(out).Should(ContainSubstring("kairos"))
			Expect(out).Should(ContainSubstring("kairos-agent"))
		} else {
			// Eventually(func() string {
			// 	out, _ := machine.SSHCommand("sudo systemctl status kairos-agent")
			// 	return out
			// }, 30*time.Second, 10*time.Second).Should(ContainSubstring("no network token"))

			out, _ := vm.Sudo("systemctl status kairos")
			Expect(out).Should(ContainSubstring("loaded (/etc/systemd/system/kairos.service; enabled; preset: disabled)"))
		}

		By("checking cmdline")
		v, err := vm.Sudo("cat /proc/cmdline")
		Expect(err).ToNot(HaveOccurred(), v)
		Expect(v).To(ContainSubstring("rd.cos.disable"))

		var fileName string
		By("waiting until the qr code is shown")
		Eventually(func() string {
			fileName = getQRImage(vm)

			return fileName
		}, 10*time.Minute, 10*time.Second).ShouldNot(BeEmpty())

		By("find the correct device (qemu vs vbox)")
		device, err := vm.Sudo(`[[ -e /dev/sda ]] && echo "/dev/sda" || echo "/dev/vda"`)
		Expect(err).ToNot(HaveOccurred(), device)

		By("registering with a screenshot")
		// pass a config that auto reboots after install as we cannot know when the machine has finished
		err = register("fatal", fileName, "./assets/config_with_reboot.yaml", strings.TrimSpace(device), true)
		Expect(err).ToNot(HaveOccurred())

		By("waiting until it reboots to installed system")
		Eventually(func() string {
			v, _ := vm.Sudo("kairos-agent state get boot")
			return strings.TrimSpace(v)
		}, 30*time.Minute, 10*time.Second).Should(ContainSubstring("active_boot"))

		Eventually(func() string {
			v, _ := vm.Sudo("cat /proc/cmdline")
			return v
		}, 10*time.Minute, 10*time.Second).ShouldNot(ContainSubstring("rd.cos.disable"))
	})
})

// getQRImage returns the path to a screenshot with a QR code or empty
// if no QR code is found
func getQRImage(vm VM) string {
	var fileName string
	image.RegisterFormat("ppm", "ppm", ppm.Decode, ppm.DecodeConfig)

	var err error
	fileName, err = vm.Screenshot()
	if err != nil {
		os.RemoveAll(fileName)
	}
	Expect(err).ToNot(HaveOccurred())

	// open and decode image file
	file, err := os.Open(fileName)
	if err != nil {
		os.RemoveAll(fileName)
	}
	img, _, err := image.Decode(file)
	if err != nil {
		os.RemoveAll(fileName)
	}
	Expect(err).ToNot(HaveOccurred())

	// prepare BinaryBitmap
	bmp, err := gozxing.NewBinaryBitmapFromImage(img)
	if err != nil {
		os.RemoveAll(fileName)
	}
	Expect(err).ToNot(HaveOccurred())

	// decode image
	qrReader := qrcode.NewQRCodeReader()
	_, err = qrReader.Decode(bmp, nil)
	if err != nil {
		os.RemoveAll(fileName)

		return ""
	}

	// Encode to png because go-nodepair doesn't understand `ppm`
	// Relevant: https://github.com/mudler/go-nodepair/pull/1
	buf := new(bytes.Buffer)
	err = png.Encode(buf, img)
	Expect(err).ToNot(HaveOccurred())

	// Replace with png data
	err = os.WriteFile(fileName, buf.Bytes(), os.ModePerm)
	Expect(err).ToNot(HaveOccurred())

	return fileName
}
