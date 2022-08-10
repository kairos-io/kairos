package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/c3os-io/c3os/internal/github"
	"github.com/c3os-io/c3os/pkg/utils"
)

func Upgrade(version, image string, force bool) error {
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

	flavor := utils.Flavor()
	if flavor == "" {
		return errors.New("no flavor detected")
	}

	registry, err := utils.OSRelease("IMAGE_REPO")
	if err != nil {
		return err
	}
	img := fmt.Sprintf("%s:%s-%s", registry, flavor, version)
	if image != "" {
		img = image
	}

	args := []string{"upgrade", "--system.uri", fmt.Sprintf("docker:%s", img)}
	cmd := exec.Command("elemental", args...)
	cmd.Env = os.Environ()
	cmd.Stdout = os.Stdout
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
