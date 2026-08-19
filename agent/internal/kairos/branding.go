package kairos

import (
	"os"
	"path"
)

func BrandingFile(s string) string {
	return path.Join("/etc", "kairos", "branding", s)
}

func DefaultTitleInteractiveInstaller() string {
	// Load it from a text file or something
	branding, err := os.ReadFile(BrandingFile("interactive_install_text"))
	if err == nil {
		return string(branding)
	} else {
		return "Kairos Interactive Installer"
	}
}
