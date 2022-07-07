package c3os

import "path"

func BrandingFile(s string) string {
	return path.Join("/etc", "c3os", "branding", s)
}
