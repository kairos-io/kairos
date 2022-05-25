package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/c3os-io/c3os/cli/github"
	"github.com/c3os-io/c3os/cli/utils"
)

func upgrade(version, image string, force bool) error {
	if version == "" && image == "" {
		releases, _ := github.FindReleases(context.Background(), "", "c3os-io/c3os")
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

	img := fmt.Sprintf("quay.io/c3os/c3os:%s-%s", flavor, version)
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
