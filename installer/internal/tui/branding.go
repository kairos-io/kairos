package tui

import (
	"os"
	"path"
)

// BrandingFile returns the path to a branding text file under /etc/kairos/branding.
func BrandingFile(s string) string {
	return path.Join("/etc", "kairos", "branding", s)
}

// DefaultTitleInteractiveInstaller returns the installer title from branding, or a default.
func DefaultTitleInteractiveInstaller() string {
	branding, err := os.ReadFile(BrandingFile("interactive_install_text"))
	if err == nil {
		return string(branding)
	}
	return "Kairos Interactive Installer"
}
