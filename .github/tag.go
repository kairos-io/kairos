package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/c3os-io/c3os/cli/github"
)

// Run it as "go run ./.github/tag.go 40" where 40 is the c3os internal version
func main() {
	releases, _ := github.FindReleases(context.Background(), "", "k3s-io/k3s")

	internalVersion := os.Args[1]
	if internalVersion == "" {
		panic("Internal version is required")
	}

	for _, v := range releases {
		if strings.Contains(v, "rc") {
			continue
		}
		v = strings.ReplaceAll(v, "+k3s1", "")
		fmt.Println(v)

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
