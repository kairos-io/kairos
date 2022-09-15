package kairos

import "path"

func BrandingFile(s string) string {
	return path.Join("/etc", "kairos", "branding", s)
}
