package cmd

import (
	"io/ioutil"

	"github.com/pterm/pterm"
)

func PrintTextFromFile(f string, banner string) {
	installText := ""
	text, err := ioutil.ReadFile(f)
	if err == nil {
		installText = string(text)
	}
	pterm.DefaultBox.WithTitle(banner).WithTitleBottomRight().WithRightPadding(0).WithBottomPadding(0).Println(
		installText)
}
