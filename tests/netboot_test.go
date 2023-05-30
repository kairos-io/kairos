package mos_test

import (
	"fmt"
	. "github.com/spectrocloud/peg/matcher"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("kairos netboot test", Label("netboot-test"), func() {
	var vm VM
	BeforeEach(func() {
		_, vm = startVM()
	})

	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			gatherLogs(vm)
			serial, _ := os.ReadFile(filepath.Join(vm.StateDir, "serial.log"))
			_ = os.MkdirAll("logs", os.ModePerm|os.ModeDir)
			_ = os.WriteFile(filepath.Join("logs", "serial.log"), serial, os.ModePerm)
			fmt.Println(string(serial))
		}
		Expect(vm.Destroy(nil)).ToNot(HaveOccurred())
	})

	It("eventually boots", func() {
		vm.EventuallyConnects(1200)
		stateAssertVM(vm, "boot", "livecd_boot")
	})
})
