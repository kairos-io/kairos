package cmd

import (
	"fmt"
	"os"

	"github.com/kairos-io/kairos/internal/kairos"

	"github.com/kairos-io/kairos-sdk/utils"
	"github.com/pterm/pterm"
)

func PrintText(f string, banner string) {
	pterm.DefaultBox.WithTitle(banner).WithTitleBottomRight().WithRightPadding(0).WithBottomPadding(0).Println(
		f)
}

func ClearScreen() {
	fmt.Print("\033c")
}

func PrintBranding(b []byte) {
	brandingFile := kairos.BrandingFile("banner")
	if _, err := os.Stat(brandingFile); err == nil {
		f, err := os.ReadFile(brandingFile)
		if err == nil {
			fmt.Println(string(f))
			return
		}
	}
	utils.PrintBanner(b)
}
