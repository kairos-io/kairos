package versioneer

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/mod/semver"
)

var ignoredImageSuffixes = []string{"-uki", "-img"}

type TagList struct {
	Tags           []string
	Artifact       *Artifact
	RegistryAndOrg string
}

// implements sort.Interface for TagList
func (tl TagList) Len() int      { return len(tl.Tags) }
func (tl TagList) Swap(i, j int) { tl.Tags[i], tl.Tags[j] = tl.Tags[j], tl.Tags[i] }
func (tl TagList) Less(i, j int) bool {
	iVersions := extractVersions(tl.Tags[i], *tl.Artifact)
	jVersions := extractVersions(tl.Tags[j], *tl.Artifact)
	iLen := len(iVersions)
	jLen := len(jVersions)

	// Drop to alphabetical order if no versions are found
	if iLen == 0 || jLen == 0 {
		return tl.Tags[i] < tl.Tags[j]
	}

	versionResult := semver.Compare(iVersions[0], jVersions[0])

	// Versions are not equal. No need to check software version.
	if versionResult != 0 {
		return versionResult == -1 // sort lower version first
	}

	// Versions are equal.
	// If there are software versions compare, otherwise return the one with
	// no software version as lower.
	if iLen == 2 && jLen == 2 {
		return semver.Compare(iVersions[1], jVersions[1]) == -1
	}

	// The one with no software version is lower
	return iLen < jLen
}

// Images returns only tags that represent images, skipping tags representing:
// - sbom
// - att
// - sig
// - -img
func (tl TagList) Images() TagList {
	pattern := `.*-(core|standard)-(amd64|arm64|riscv64)-.*-v.*`
	regexpObject := regexp.MustCompile(pattern)

	newTags := []string{}
	for _, t := range tl.Tags {
		// Golang regexp doesn't support negative lookaheads so we filter some images
		// outside regexp.
		if regexpObject.MatchString(t) && !ignoreSuffixedTag(t) {
			newTags = append(newTags, t)
		}
	}

	return newTagListWithTags(tl, newTags)
}

// OtherVersions returns tags that match all fields of the given Artifact,
// except the Version. Should be used to return other possible versions for the same
// Kairos image (e.g. that one could upgrade to).
// This method returns all versions, not only newer ones. Use NewerVersions to
// fetch only versions, newer than the one of the Artifact.
func (tl TagList) OtherVersions() TagList {
	sVersionForTag := tl.Artifact.SoftwareVersionForTag()

	newTags := []string{}
	for _, t := range tl.Images().Tags {
		versions := extractVersions(t, *tl.Artifact)
		if len(versions) > 0 && versions[0] != tl.Artifact.Version {
			if len(versions) > 1 && versions[1] != sVersionForTag {
				continue
			}

			newTags = append(newTags, t)
		}
	}

	return newTagListWithTags(tl, newTags)
}

// NewerVersions returns OtherVersions filtered to only include tags with
// Version higher than the given artifact's.
func (tl TagList) NewerVersions() TagList {
	tags := tl.OtherVersions()

	return tags.newerVersions()
}

// OtherSoftwareVersions returns tags that match all fields of the given Artifact,
// except the SoftwareVersion. Should be used to return other possible software versions
// for the same Kairos image (e.g. that one could upgrade to).
// This method returns all versions, not only newer ones. Use NewerSofwareVersions to
// fetch only versions, newer than the one of the Artifact.
func (tl TagList) OtherSoftwareVersions() TagList {
	versionForTag := tl.Artifact.VersionForTag()
	softwareVersionForTag := tl.Artifact.SoftwareVersionForTag()

	newTags := []string{}
	for _, t := range tl.Images().Tags {
		versions := extractVersions(t, *tl.Artifact)
		if len(versions) > 1 && versions[1] != softwareVersionForTag && versions[0] == versionForTag {
			newTags = append(newTags, t)
		}
	}

	return newTagListWithTags(tl, newTags)
}

// NewerSofwareVersions returns OtherSoftwareVersions filtered to only include tags with
// SoftwareVersion higher than the given artifact's.
func (tl TagList) NewerSofwareVersions() TagList {
	tags := tl.OtherSoftwareVersions()

	return tags.newerSoftwareVersions()
}

// OtherAnyVersion returns tags that match all fields of the given Artifact,
// except the SoftwareVersion and/or Version.
// Should be used to return tags with newer versions (Kairos or "software")
// that one could upgrade to.
// This method returns all versions, not only newer ones. Use NewerAnyVersion to
// fetch only versions, newer than the one of the Artifact.
func (tl TagList) OtherAnyVersion() TagList {
	versionForTag := tl.Artifact.VersionForTag()
	sVersionForTag := tl.Artifact.SoftwareVersionForTag()

	newTags := []string{}
	for _, t := range tl.Images().Tags {
		versions := extractVersions(t, *tl.Artifact)
		versionDiffers := len(versions) > 0 && versions[0] != versionForTag
		sVersionDiffers := len(versions) > 1 && versions[1] != sVersionForTag
		if versionDiffers || sVersionDiffers {
			newTags = append(newTags, t)
		}
	}
	return newTagListWithTags(tl, newTags)
}

// NewerAnyVersion returns tags with:
//   - a kairos version newer than the given artifact's
//   - a kairos version same as the given artifacts but a software version higher
//     than the current artifact's
//
// Splitting the 2 versions is done using the artifact's SoftwareVersionPrefix
// (first encountered, because our tags have a "k3s1" in the end too)
func (tl TagList) NewerAnyVersion() TagList {
	if tl.Artifact.SoftwareVersion != "" {
		return tl.Images().newerSomeVersions()
	} else {
		return tl.Images().newerVersions()
	}
}

func (tl TagList) Print() {
	for _, t := range tl.Tags {
		fmt.Println(t)
	}
}

// FullImages returns a slice of strings which has the tags converts to full
// image URLs (not just tags).
func (tl TagList) FullImages() ([]string, error) {
	result := []string{}
	if tl.Artifact == nil {
		return result, errors.New("no artifact defined")
	}

	repo := tl.Artifact.Repository(tl.RegistryAndOrg)
	for _, t := range tl.Tags {
		result = append(result, fmt.Sprintf("%s:%s", repo, t))
	}

	return result, nil
}

func (tl TagList) PrintImages() {
	fullImages, err := tl.FullImages()
	if err != nil {
		fmt.Printf("warn: %s\n", err.Error())
	}
	for _, t := range fullImages {
		fmt.Println(t)
	}
}

// Sorted returns the TagList sorted by semver.
// This means lower versions come first.
func (tl TagList) Sorted() TagList {
	newTagList := newTagListWithTags(tl, tl.Tags)
	sort.Sort(newTagList)

	return newTagList
}

// RSorted returns the TagList in the reverse order of Sorted
// This means higher versions come first.
func (tl TagList) RSorted() TagList {
	newTagList := newTagListWithTags(tl, tl.Tags)
	sort.Sort(sort.Reverse(newTagList))

	return newTagList
}

func (tl TagList) newerVersions() TagList {
	newTags := []string{}
	for _, t := range tl.Tags {
		versions := extractVersions(t, *tl.Artifact)
		if len(versions) > 0 && semver.Compare(versions[0], tl.Artifact.VersionForTag()) == +1 {
			newTags = append(newTags, t)
		}
	}

	return newTagListWithTags(tl, newTags)
}

func (tl TagList) newerSoftwareVersions() TagList {
	newTags := []string{}
	for _, t := range tl.Tags {
		versions := extractVersions(t, *tl.Artifact)
		if len(versions) > 1 && semver.Compare(versions[1], tl.Artifact.SoftwareVersionForTag()) == +1 {
			newTags = append(newTags, t)
		}
	}

	return newTagListWithTags(tl, newTags)
}

func (tl TagList) newerSomeVersions() TagList {
	newTags := []string{}
	for _, t := range tl.Tags {
		versions := extractVersions(t, *tl.Artifact)
		// skip badly named artifacts that may not have a software version
		// https://github.com/kairos-io/kairos/issues/3167#issuecomment-2633282993
		if len(versions) < 2 {
			continue
		}

		versionResult := semver.Compare(versions[0], tl.Artifact.VersionForTag())
		sVersionResult := semver.Compare(versions[1], tl.Artifact.SoftwareVersionForTag())

		// If kairos version is higher add it (no matter what the sversion is)
		if versionResult > 0 {
			newTags = append(newTags, t)
		}

		// if kairos version is the same, require the sversion to be higher
		if versionResult == 0 && sVersionResult > 0 {
			newTags = append(newTags, t)
		}
	}

	return newTagListWithTags(tl, newTags)
}

// NoPrereleases returns only tags in which Version is not a pre-release (as defined by semver).
// NOTE: We only filter out Kairos prereleases because the k3s version is not
// semver anyway. The upstream version is something like: v1.28.3+k3s2
// The first part is semver and it's the Kubernetes version and the "+k3s2"
// part is the k3s version which has changes over "k3s1" (it's not just a new build
// of the same thing)(https://github.com/k3s-io/k3s/releases/tag/v1.28.3%2Bk3s2)
// To make things more complicated, when we create a container image tag, we
// convert "+" to "-" because tags don't allow "+" symbols. This makes every
// k3s version look like a pre-release according to semver.
func (tl TagList) NoPrereleases() TagList {
	newTags := []string{}
	for _, t := range tl.Tags {
		versions := extractVersions(t, *tl.Artifact)

		noVersionsFound := len(versions) == 0
		lessVersionsFound := tl.Artifact.SoftwareVersion != "" && len(versions) < 2
		if noVersionsFound || lessVersionsFound {
			continue
		}

		versionIsPrerelease := semver.IsValid(versions[0]) && semver.Prerelease(versions[0]) != ""
		if versionIsPrerelease {
			continue
		}

		newTags = append(newTags, t)
	}

	return newTagListWithTags(tl, newTags)
}

// extractVersions extracts extractVersions from a given tag, based on the given Artifact
// E.g. for an artifact like:
// leap-15.5-core-amd64-generic-v2.4.2-rc1-k3sv1.28.3-k3s1
// given a tagToCheck like:
// leap-15.5-core-amd64-generic-v2.4.3-k3sv1.28.6-k3s1
// it should return:
// []string{"v2.4.3", "v1.28.6-k3s1"}
//
// Or, for an artifact like:
// leap-15.5-core-amd64-generic-v2.4.2
// given a tagToCheck like:
// leap-15.5-core-amd64-generic-v2.4.3
// it should return:
// []string{"v2.4.3"}
//
// - check if there are 2 extractVersions in the tag and return both
// - if there is only one, return that (Version)
// - otherwise return no version
func extractVersions(tagToCheck string, artifact Artifact) []string {
	tag, err := artifact.Tag()
	if err != nil {
		panic(fmt.Errorf("invalid artifact passed: %w", err))
	}

	// Remove all version information
	cleanupPattern := fmt.Sprintf("-%s.*", artifact.Version)
	re := regexp.MustCompile(cleanupPattern)
	strippedTag := re.ReplaceAllString(tag, "")

	if artifact.SoftwareVersionPrefix != "" { // If we know how to split the versions
		// Construct a regexp for both versions and check if there is match
		pattern := fmt.Sprintf("%s-(.+?)-%s(.+)", regexp.QuoteMeta(strippedTag), artifact.SoftwareVersionPrefix)
		regexpObj := regexp.MustCompile(pattern)
		matches := regexpObj.FindStringSubmatch(tagToCheck)

		if len(matches) == 3 {
			return matches[1:]
		}
	}

	// Construct a regexp for one version and check if there is a match
	pattern := fmt.Sprintf("%s-(.+)", regexp.QuoteMeta(strippedTag))
	regexpObj := regexp.MustCompile(pattern)
	matches := regexpObj.FindStringSubmatch(tagToCheck)

	if len(matches) == 2 {
		subSlice := make([]string, 1)
		copy(subSlice, matches[1:])
		return subSlice
	}

	// No version found
	return []string{}
}

// newTagListWithTags returns a copy of the given TagList with same Artifact
// and RegistryAndOrg fields but with the given tags as Tags.
func newTagListWithTags(tl TagList, tags []string) TagList {
	return TagList{Artifact: tl.Artifact, RegistryAndOrg: tl.RegistryAndOrg, Tags: tags}
}

func ignoreSuffixedTag(tag string) bool {
	for _, i := range ignoredImageSuffixes {
		if strings.HasSuffix(tag, i) {
			return true
		}
	}
	return false
}
