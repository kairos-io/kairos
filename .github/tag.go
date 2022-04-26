package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/c3os-io/c3os/cli/github"
	"github.com/hashicorp/go-version"
)

// Run it as "go run ./.github/tag.go 40" where 40 is the c3os internal version
func main() {
	releases, _ := github.FindReleases(context.Background(), "", "k3s-io/k3s")

	internalVersion := os.Args[1]
	if internalVersion == "" {
		panic("Internal version is required")
	}

	minorV := func(segments []int) string {
		return fmt.Sprintf("%d.%d", segments[0], segments[1])
	}

	processed := map[string][]string{}
	toprocess := []string{}
	for _, v := range releases {

		if strings.Contains(v, "rc") {
			continue
		}

		v = strings.ReplaceAll(v, "+k3s1", "")
		semver, err := version.NewVersion(v)
		if err != nil {
			fmt.Println("Skipping", v, "not semver")
			continue
		}
		segments := semver.Segments()

		processed[minorV(segments)] = append(processed[minorV(segments)], v)

	}

	for _, v := range processed {
		versions := make([]*version.Version, len(v))
		for i, raw := range v {
			v, _ := version.NewVersion(raw)
			versions[i] = v
		}

		// After this, the versions are properly sorted
		sort.Sort(version.Collection(versions))
		toprocess = append(toprocess, versions[len(versions)-1].Original())
	}

	for _, v := range toprocess {
		expectedTag := fmt.Sprintf("%s-%s", v, internalVersion)
		if !checkTag(expectedTag) {
			fmt.Println(expectedTag, "missing")
			if os.Getenv("DRY_RUN") != "" {
				continue
			}

			out, err := exec.Command("git", "tag", expectedTag).CombinedOutput()
			if err != nil {
				panic(err)
			}
			fmt.Println(string(out))

			if os.Getenv("NO_PUSH") != "" {
				continue
			}

			out, err = exec.Command("git", "push", "origin", expectedTag).CombinedOutput()
			if err != nil {
				panic(err)
			}
			fmt.Println(string(out))

		} else {
			fmt.Println(expectedTag, "present")
		}
	}
}

func checkTag(v string) bool {
	return exec.Command("git", "rev-list", v+"..").Run() == nil
}
