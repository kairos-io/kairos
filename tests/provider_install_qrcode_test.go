// nolint
package mos_test

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"net"
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

// Every wait below is capped at 10 minutes, so the spec cannot outlast the
// 60 minute suite budget however it fails (#4489). A green run of this spec
// costs 303s end to end, and its slowest step, the pairing handshake, costs
// 191s, so 10 minutes leaves every step at least 3x its measured cost.
var _ = Describe("kairos qr code install", Label("provider", "provider-qrcode-install"), func() {
	var vm VM

	BeforeEach(func() {
		_, vm = startVM()
		vm.EventuallyConnects(600)
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

		expectDefaultService(vm)

		By("checking cmdline")
		v, err := vm.Sudo("cat /proc/cmdline")
		Expect(err).ToNot(HaveOccurred(), v)
		Expect(v).To(ContainSubstring("rd.cos.disable"))

		// The live ISO boots into the interactive installer, whose welcome
		// page draws a QR code of the web UI address. The pairing QR only
		// shows up once "a" hands the terminal to the agent's pairing
		// install, so a URL in the code means the key still has to be sent.
		var fileName string
		By("waiting until the pairing qr code is shown")
		Eventually(func() string {
			var text string
			fileName, text = getQRImage(vm)
			if strings.HasPrefix(text, "http") {
				fmt.Printf("Found the web UI QR code (%s), starting the pairing install\n", text)
				Expect(sendKey(vm, "a")).To(Succeed())
				os.RemoveAll(fileName)
				return ""
			}

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
		}, 10*time.Minute, 10*time.Second).Should(ContainSubstring("active_boot"))

		Eventually(func() string {
			v, _ := vm.Sudo("cat /proc/cmdline")
			return v
		}, 10*time.Minute, 10*time.Second).ShouldNot(ContainSubstring("rd.cos.disable"))
	})
})

// sendKey presses key on the VM's keyboard through the qemu monitor socket,
// the same socket peg takes screenshots through.
func sendKey(vm VM, key string) error {
	conn, err := net.Dial("unix", filepath.Join(vm.StateDir, "qemu-monitor.sock"))
	if err != nil {
		return err
	}
	defer conn.Close()

	if _, err := fmt.Fprintf(conn, "sendkey %s\r\n", key); err != nil {
		return err
	}

	// Like screendump, the command only takes effect once the monitor's
	// reply is read, so drain the socket until it goes quiet.
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		return err
	}
	b := make([]byte, 1024)
	for {
		if _, err := conn.Read(b); err != nil {
			return nil
		}
	}
}

// getQRImage returns the path to a screenshot with a QR code and the text
// the code holds, or empty strings if no QR code is found
func getQRImage(vm VM) (string, string) {
	var fileName string
	image.RegisterFormat("ppm", "ppm", ppm.Decode, ppm.DecodeConfig)

	var err error
	fileName, err = vm.Screenshot()
	if err != nil {
		fmt.Printf("Error taking screenshot: %s\n", err.Error())
		os.RemoveAll(fileName)
	}
	Expect(err).ToNot(HaveOccurred())
	fmt.Printf("Screenshot saved to %s\n", fileName)

	// open and decode image file
	file, err := os.Open(fileName)
	if err != nil {
		fmt.Printf("Error opening file %s: %s\n", fileName, err.Error())
		os.RemoveAll(fileName)
	}
	img, _, err := image.Decode(file)
	if err != nil {
		fmt.Printf("Error decoding image %s: %s\n", fileName, err.Error())
		os.RemoveAll(fileName)
	}
	Expect(err).ToNot(HaveOccurred())

	// prepare BinaryBitmap
	bmp, err := gozxing.NewBinaryBitmapFromImage(img)
	if err != nil {
		fmt.Printf("Error creating BinaryBitmap from image %s: %s\n", fileName, err.Error())
		os.RemoveAll(fileName)
	}
	Expect(err).ToNot(HaveOccurred())

	// decode image
	qrReader := qrcode.NewQRCodeReader()
	result, err := qrReader.Decode(bmp, nil)
	if err != nil {
		fmt.Printf("Error decoding QR code from image %s: %s\n", fileName, err.Error())
		os.RemoveAll(fileName)

		return "", ""
	}

	// Encode to png because go-nodepair doesn't understand `ppm`
	// Relevant: https://github.com/mudler/go-nodepair/pull/1
	buf := new(bytes.Buffer)
	err = png.Encode(buf, img)
	Expect(err).ToNot(HaveOccurred())

	// Replace with png data
	err = os.WriteFile(fileName, buf.Bytes(), os.ModePerm)
	Expect(err).ToNot(HaveOccurred())

	return fileName, result.GetText()
}
