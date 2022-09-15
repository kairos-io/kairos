package mos_test

import (
	"fmt"
	"os"
	"time"

	"github.com/kairos-io/kairos/pkg/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("kairos qr code register", Label("qrcode-register"), func() {

	Context("register", func() {
		It("sends config over", func() {
			download("https://github.com/schollz/croc/releases/download/v9.6.0/croc_9.6.0_Linux-64bit.tar.gz")
			Eventually(func() error {
				os.RemoveAll("screenshot.png")
				out, err := utils.SH(fmt.Sprintf("./croc --yes %s", os.Getenv("SENDKEY")))
				fmt.Println(out)
				if err != nil {
					return err
				}

				out, err = utils.SH(fmt.Sprintf("kairos register --device /dev/sda --config %s %s", os.Getenv("CLOUD_INIT"), "screenshot.png"))
				fmt.Println(out)
				return err

			}, 20*time.Minute, 10*time.Second).ShouldNot(HaveOccurred())
		})
	})
})
