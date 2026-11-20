package auroraboot

import "regexp"

const defaultKairosInitImage = "quay.io/kairos/kairos-init"

// releaseVersion matches the tag of a Kairos release or release candidate. A
// build between tags (v4.5.1-5-gabc1234) and a dirty tree do not match, because
// no kairos-init image carries such a tag.
var releaseVersion = regexp.MustCompile(`^v\d+\.\d+\.\d+(-[A-Za-z]+\.?\d+)?$`)

// KairosInitImageFor returns the kairos-init image a build uses when nothing
// names one. AuroraBoot is released under the Kairos version, and kairos-init
// is released under the same tag, so a release build uses the kairos-init of its
// own version. A build with no release version, such as a source build or a
// binary built without ldflags, uses the latest release.
func KairosInitImageFor(version string) string {
	if version == "v0.0.0" || !releaseVersion.MatchString(version) {
		return defaultKairosInitImage + ":latest"
	}
	return defaultKairosInitImage + ":" + version
}
