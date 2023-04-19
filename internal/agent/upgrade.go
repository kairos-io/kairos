package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Masterminds/semver/v3"
	"os"
	"os/exec"
	"strings"

	events "github.com/kairos-io/kairos-sdk/bus"
	"github.com/kairos-io/kairos/v2/pkg/config"
	"github.com/kairos-io/kairos/v2/pkg/config/collector"

	"github.com/kairos-io/kairos-sdk/utils"
	"github.com/kairos-io/kairos/v2/internal/bus"
	"github.com/kairos-io/kairos/v2/pkg/github"
	"github.com/mudler/go-pluggable"
)

func ListReleases() semver.Collection {
	var releases semver.Collection

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

func Upgrade(
	version, image string, force, debug, strictValidations bool, dirs []string,
	authUser string, authPass string, authServer string, authType string, registryToken string, identityToken string,
) error {
	bus.Manager.Initialize()

	if version == "" && image == "" {
		releases := ListReleases()

		if len(releases) == 0 {
			return fmt.Errorf("no releases found")
		}

		// Using Original here because the parsing removes the v as its a semver. But it stores the original full version there
		version = releases[0].Original()

		if utils.Version() == version && !force {
			fmt.Printf("version %s already installed. use --force to force upgrade\n", version)
			return nil
		}
		msg := fmt.Sprintf("Latest release is %s\nAre you sure you want to upgrade to this release? (y/n)", version)
		reply, err := promptBool(events.YAMLPrompt{Prompt: msg, Default: "y"})
		if err != nil {
			return err
		}
		if reply == "false" {
			return nil
		}
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

	c, err := config.Scan(collector.Directories(dirs...), collector.StrictValidation(strictValidations))
	if err != nil {
		return err
	}

	utils.SetEnv(c.Env)

	args := []string{"upgrade", "--system.uri", fmt.Sprintf("docker:%s", img)}
	args = append(args,
		"--auth-username", authUser,
		"--auth-password", authPass,
		"--auth-server-address", authServer,
		"--auth-type", authType,
		"--auth-registry-token", registryToken,
		"--auth-identity-token", identityToken,
	)

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
