package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/kairos-io/kairos/pkg/config"
	events "github.com/kairos-io/kairos/sdk/bus"

	"github.com/kairos-io/kairos/internal/bus"
	"github.com/kairos-io/kairos/pkg/github"
	"github.com/kairos-io/kairos/pkg/utils"
	"github.com/mudler/go-pluggable"
)

func ListReleases() []string {
	releases := []string{}

	bus.Manager.Response(events.EventAvailableReleases, func(p *pluggable.Plugin, r *pluggable.EventResponse) {
		if err := json.Unmarshal([]byte(r.Data), &releases); err != nil {
			fmt.Printf("warn: failed unmarshalling data: '%s'\n", err.Error())
		}
	})

	if _, err := bus.Manager.Publish(events.EventAvailableReleases, events.EventPayload{}); err != nil {
		fmt.Printf("warn: failed publishing event: '%s'\n", err.Error())
	}

	if len(releases) == 0 {
		githubRepo, err := utils.OSRelease("GITHUB_REPO")
		if err != nil {
			return releases
		}
		releases, _ = github.FindReleases(context.Background(), "", githubRepo)
	}

	return releases
}

func Upgrade(version, image string, force, debug bool, dirs []string) error {
	bus.Manager.Initialize()

	if version == "" && image == "" {
		releases := ListReleases()

		if len(releases) == 0 {
			return fmt.Errorf("no releases found")
		}

		version = releases[0]
		fmt.Println("latest release is ", version)
	}

	if utils.Version() == version && !force {
		fmt.Println("version already installed. use --force to force upgrade")
		return nil
	}

	discoveredImage := ""
	bus.Manager.Response(events.EventVersionImage, func(p *pluggable.Plugin, r *pluggable.EventResponse) {
		discoveredImage = r.Data
	})

	_, err := bus.Manager.Publish(events.EventVersionImage, &events.VersionImagePayload{
		Version: version,
	})
	if err != nil {
		return err
	}

	registry, err := utils.OSRelease("IMAGE_REPO")
	if err != nil {
		return err
	}

	img := fmt.Sprintf("%s:%s", registry, version)
	if discoveredImage != "" {
		img = discoveredImage
	}
	if image != "" {
		img = image
	}

	if debug {
		fmt.Printf("Upgrading to image: '%s'\n", img)
	}

	c, err := config.Scan(config.Directories(dirs...))
	if err != nil {
		return err
	}

	utils.SetEnv(c.Env)

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
