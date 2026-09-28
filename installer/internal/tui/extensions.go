package tui

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	sdkCatalog "github.com/kairos-io/kairos/v4/sdk/extensions"
	sdkExtensions "github.com/kairos-io/kairos/v4/sdk/types/extensions"
)

// liveMediaDir is where the ISO root is mounted while the installer runs, on
// both the GRUB and the UKI flow. AuroraBoot lands an `iso.overlay_iso` tree at
// the ISO root, so dropping a `<name>.sysext.raw` next to the ISO's config.yaml
// is how an artifact ships an extension with no network at install time.
//
// It is a variable so the tests can point it at a temporary directory.
var liveMediaDir = "/run/initramfs/live"

// liveExtensionSuffix is the file name suffix that marks an extension image
// shipped on the live media. It is the same suffix the sysext tooling and the
// catalog use, so an image published to a catalog can be dropped on an ISO
// unchanged.
const liveExtensionSuffix = ".sysext.raw"

// catalogTimeout bounds the catalog fetch. The installer is interactive, and a
// machine that booted with no route to the catalog host must not leave the user
// looking at a spinner: what the live media carries is offered either way.
const catalogTimeout = 15 * time.Second

// extensionOrigin names where an offered extension came from, so the screen can
// say which entries work with no network.
const (
	originLiveMedia = "live media"
)

// extensionChoice is one extension the screen offers.
type extensionChoice struct {
	// Label is what the screen shows.
	Label string
	// Name is what goes into install.extensions[].name: an absolute path for a
	// live media image, a catalog name otherwise.
	Name string
	// Version is the catalog version to pin to. Empty means the newest version
	// the catalog publishes, which is what AuroraBoot writes when the build
	// leaves an extension on latest.
	Version string
	// Origin is the live media, or the repository of the catalog that
	// publishes the extension.
	Origin string
	// Latest is the newest version the catalog publishes, shown for
	// information only. It is empty for a live media image.
	Latest string
}

// liveMediaExtensions lists the extension images shipped on the live media
// under root.
//
// A missing directory is not an error: an install that did not boot from
// removable media has no live directory at all.
func liveMediaExtensions(root string) ([]extensionChoice, error) {
	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var found []extensionChoice
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), liveExtensionSuffix) {
			return nil
		}
		found = append(found, extensionChoice{
			Label:  strings.TrimSuffix(entry.Name(), liveExtensionSuffix),
			Name:   path,
			Origin: originLiveMedia,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return found, nil
}

// catalogExtensions lists every layer the catalogs at urls publish.
//
// A catalog that cannot be fetched or parsed is skipped rather than failing the
// call, the same contract the agent's FetchCatalogs follows: with several
// catalogs configured, one unreachable index must not hide all the others. The
// error is returned only when no catalog at all could be read, so the caller
// can say "no network" instead of "no extensions published".
func catalogExtensions(ctx context.Context, client *http.Client, urls []string) ([]extensionChoice, error) {
	var (
		found    []extensionChoice
		failures []error
		read     int
	)
	for _, url := range urls {
		catalog, err := fetchCatalog(ctx, client, url)
		if err != nil {
			failures = append(failures, fmt.Errorf("catalog %s: %w", url, err))
			continue
		}
		read++
		for _, layer := range catalog.Layers {
			found = append(found, extensionChoice{
				Label:  layer.Name,
				Name:   layer.Name,
				Origin: catalog.Repository,
				Latest: layer.Latest,
			})
		}
	}
	if read == 0 && len(failures) > 0 {
		return nil, fmt.Errorf("no extension catalog could be read: %w", errors.Join(failures...))
	}
	return found, nil
}

func fetchCatalog(ctx context.Context, client *http.Client, url string) (sdkCatalog.Catalog, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return sdkCatalog.Catalog{}, err
	}
	response, err := client.Do(request)
	if err != nil {
		return sdkCatalog.Catalog{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return sdkCatalog.Catalog{}, fmt.Errorf("got status %s", response.Status)
	}
	return sdkCatalog.Parse(response.Body)
}

// discoverExtensions returns the union of the extensions on the live media and
// the ones the catalogs publish, sorted by label.
//
// The live media wins a name it shares with a catalog entry: it is already on
// the machine, so selecting it installs with no network, which is the case the
// catalog entry cannot serve. The returned error reports that no catalog could
// be read; the live media entries are returned with it, because an airgapped
// install has to keep working.
func discoverExtensions(ctx context.Context, client *http.Client, root string, urls []string) ([]extensionChoice, error) {
	live, err := liveMediaExtensions(root)
	if err != nil {
		return nil, err
	}

	choices := live
	taken := map[string]bool{}
	for _, choice := range live {
		taken[choice.Label] = true
	}

	published, catalogErr := catalogExtensions(ctx, client, urls)
	for _, choice := range published {
		if taken[choice.Label] {
			continue
		}
		taken[choice.Label] = true
		choices = append(choices, choice)
	}

	sort.Slice(choices, func(i, j int) bool { return choices[i].Label < choices[j].Label })
	return choices, catalogErr
}

// selectedExtensions turns the chosen entries into the install.extensions list
// the agent's install hook reads.
func selectedExtensions(choices []extensionChoice, selected map[int]bool) sdkExtensions.Extensions {
	var chosen sdkExtensions.Extensions
	for i, choice := range choices {
		if !selected[i] {
			continue
		}
		chosen = append(chosen, sdkExtensions.Extension{Name: choice.Name, Version: choice.Version})
	}
	return chosen
}
