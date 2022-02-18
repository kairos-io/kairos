package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/c3os-io/c3os/installer/github"
	"github.com/c3os-io/c3os/installer/utils"
)

func upgrade(version string, force bool) error {
	if version == "" {
		releases, _ := github.FindReleases(context.Background(), "", "c3os-io/c3os")
		version = releases[len(releases)-1]
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

	args := []string{"--no-verify", "--no-cosign", "--docker-image", fmt.Sprintf("quay.io/c3os/c3os:%s-%s", flavor, version)}

	cmd := exec.Command("cos-upgrade", args...)
	cmd.Env = os.Environ()
	cmd.Stdout = os.Stdout
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
