package cmd

import (
	"fmt"
	"io/ioutil"
	"os"

	"github.com/kairos-io/kairos/internal/kairos"

	"github.com/kairos-io/kairos/pkg/utils"
	"github.com/pterm/pterm"
)

func PrintText(f string, banner string) {
	pterm.DefaultBox.WithTitle(banner).WithTitleBottomRight().WithRightPadding(0).WithBottomPadding(0).Println(
		f)
}

func PrintBranding(b []byte) {
	brandingFile := kairos.BrandingFile("banner")
	if _, err := os.Stat(brandingFile); err == nil {
		f, err := ioutil.ReadFile(brandingFile)
		if err == nil {
			fmt.Println(string(f))
		}

	}
	utils.PrintBanner(b)
}
