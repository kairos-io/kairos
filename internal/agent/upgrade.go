package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/c3os-io/c3os/internal/github"
	"github.com/c3os-io/c3os/pkg/utils"
)

func Upgrade(version, image string, force, debug bool) error {
	if version == "" && image == "" {
		githubRepo, err := utils.OSRelease("GITHUB_REPO")
		if err != nil {
			return err
		}
		releases, _ := github.FindReleases(context.Background(), "", githubRepo)
		version = releases[0]
		fmt.Println("latest release is ", version)
	}

	if utils.Version() == version && !force {
		fmt.Println("latest version already installed. use --force to force upgrade")
		return nil
	}

	registry, err := utils.OSRelease("IMAGE_REPO")
	if err != nil {
		return err
	}
	img := fmt.Sprintf("%s:%s", registry, version)
	if image != "" {
		img = image
	}

	if debug {
		fmt.Printf("Upgrading to image: '%s'\n", img)
	}

	args := []string{"upgrade", "--system.uri", fmt.Sprintf("docker:%s", img)}

	if debug {
		fmt.Printf("Running command: 'elemental %s'", strings.Join(args, " "))
	}

	cmd := exec.Command("elemental", args...)
	cmd.Env = os.Environ()
	cmd.Stdout = os.Stdout
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
