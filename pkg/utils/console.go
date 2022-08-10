package utils

import (
	"bufio"
	"bytes"
	"fmt"
	"image"
	"os"
	"strings"

	"github.com/pterm/pterm"
	"github.com/qeesung/image2ascii/convert"
)

func Prompt(t string) (string, error) {
	if t != "" {
		pterm.Info.Println(t)
	}
	answer, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(answer), nil
}

func PrintBanner(d []byte) {
	img, _, _ := image.Decode(bytes.NewReader(d))

	convertOptions := convert.DefaultOptions
	convertOptions.FixedWidth = 100
	convertOptions.FixedHeight = 40

	// Create the image converter
	converter := convert.NewImageConverter()
	fmt.Print(converter.Image2ASCIIString(img, &convertOptions))
}
