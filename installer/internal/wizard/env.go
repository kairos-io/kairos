package wizard

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/kairos-io/kairos/v4/sdk/branding"
	sdkBus "github.com/kairos-io/kairos/v4/sdk/bus"
	sdkExtensions "github.com/kairos-io/kairos/v4/sdk/types/extensions"
	"github.com/mudler/go-pluggable"

	"github.com/kairos-io/kairos/v4/installer/internal/disks"
)

// zoneinfoDir and keymapDirs are variables so the tests can point them at a
// temporary tree.
var (
	zoneinfoDir = "/usr/share/zoneinfo"
	keymapDirs  = []string{"/usr/share/keymaps", "/usr/share/kbd/keymaps", "/usr/lib/kbd/keymaps", "/lib/kbd/keymaps"}
)

// SystemEnv answers from the machine the installer runs on.
type SystemEnv struct {
	LiveMediaDir string
	Catalogs     []string
	Client       *http.Client
	// Architecture is the one a catalog entry has to publish an extension
	// image for to be worth offering. The installer runs on the machine it
	// installs, so it is the architecture of this binary.
	Architecture string
}

// NewSystemEnv returns the production Env.
func NewSystemEnv() *SystemEnv {
	return &SystemEnv{
		LiveMediaDir: liveMediaDir,
		Catalogs:     sdkExtensions.Config{}.CatalogURLs(),
		Client:       &http.Client{Timeout: catalogTimeout},
		Architecture: runtime.GOARCH,
	}
}

func (e *SystemEnv) Disks() ([]disks.Disk, error) { return disks.Scan() }

func (e *SystemEnv) Extensions(ctx context.Context) ([]Choice, error) {
	ctx, cancel := context.WithTimeout(ctx, catalogTimeout)
	defer cancel()
	found, err := discoverExtensions(ctx, e.Client, e.LiveMediaDir, e.Catalogs, e.Architecture)
	out := make([]Choice, 0, len(found))
	for _, c := range found {
		out = append(out, c.toChoice())
	}
	return out, err
}

// Timezones reads the zone table tzdata ships, which lists the canonical
// zones only, not the aliases and posix/ right/ copies a directory walk
// would find. UTC is not in it and is always offered first.
func (e *SystemEnv) Timezones() []string {
	for _, table := range []string{"zone1970.tab", "zone.tab"} {
		f, err := os.Open(filepath.Join(zoneinfoDir, table))
		if err != nil {
			continue
		}
		defer f.Close()
		zones := []string{}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := sc.Text()
			if strings.HasPrefix(line, "#") {
				continue
			}
			cols := strings.Split(line, "\t")
			if len(cols) >= 3 {
				zones = append(zones, cols[2])
			}
		}
		sort.Strings(zones)
		return append([]string{"UTC"}, zones...)
	}
	return nil
}

// Keymaps lists the console keymaps the image carries, by name.
func (e *SystemEnv) Keymaps() []string {
	seen := map[string]bool{}
	for _, dir := range keymapDirs {
		_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			name := d.Name()
			for _, ext := range []string{".map.gz", ".kmap.gz", ".map", ".kmap"} {
				if strings.HasSuffix(name, ext) {
					seen[strings.TrimSuffix(name, ext)] = true
				}
			}
			return nil
		})
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ProviderPrompts asks the installed provider plugins what they want to ask.
// This is the code that lived in the TUI's customization page.
func (e *SystemEnv) ProviderPrompts() []sdkBus.YAMLPrompt {
	bus := sdkBus.NewBus()
	bus.Initialize()
	var prompts []sdkBus.YAMLPrompt
	bus.Response(sdkBus.EventInteractiveInstall, func(_ *pluggable.Plugin, resp *pluggable.EventResponse) {
		if resp.Data == "" {
			return
		}
		var r []sdkBus.YAMLPrompt
		if err := json.Unmarshal([]byte(resp.Data), &r); err == nil {
			prompts = append(prompts, r...)
		}
	})
	_, _ = bus.Publish(sdkBus.EventInteractiveInstall, sdkBus.EventPayload{})
	return prompts
}

func (e *SystemEnv) AdvancedDisabled() bool {
	_, err := os.Stat(branding.File("interactive_install_advanced_disabled"))
	return err == nil
}
