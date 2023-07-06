package main

import (
	"fmt"
	"github.com/urfave/cli"
	"gopkg.in/yaml.v3"
	"os"
	"sort"
	"text/template"
)

func main() {

	app := &cli.App{
		Name:      "repochanges",
		Version:   "0.0.1",
		Authors:   []cli.Author{{Name: "Kairos authors"}},
		Usage:     "Extract changes betweeen 2 luet repository version list files",
		UsageText: `repochanges OLDREFENRENCE NEWREFERENCE`,
		Copyright: "kairos authors",
		ArgsUsage: "oldreference newreference",
		Before: func(context *cli.Context) error {
			if context.NArg() != 2 {
				return fmt.Errorf("not enough arguments")
			}
			return nil
		},
		Action: func(c *cli.Context) error {
			var changes []Changed

			file1, err := os.ReadFile(c.Args().Get(0))
			if err != nil {
				return err
			}
			file2, err := os.ReadFile(c.Args().Get(1))
			if err != nil {
				return err
			}

			var oldVersions []Package
			var newVersions []Package

			_ = yaml.Unmarshal(file1, &oldVersions)
			_ = yaml.Unmarshal(file2, &newVersions)

			for _, p := range newVersions {
				in, oldpkg := isIn(p, oldVersions)
				if !in {
					// New package
					changes = append(changes, Changed{
						Name:     p.Name,
						Category: p.Category,
						From:     "0",
						To:       p.Version,
						New:      true,
					})
				} else {
					// Updated
					if p.Version != oldpkg.Version {
						changes = append(changes, Changed{
							Name:     p.Name,
							Category: p.Category,
							From:     oldpkg.Version,
							To:       p.Version,
							New:      false,
						})
					}
				}
			}

			// Sort by newer package first
			sort.Slice(changes, func(i, j int) bool {
				return changes[i].New
			})
			tmpl, err := template.New("test").Parse("{{range .}}{{ if .New }}[New]{{else}}[Update]{{end}} {{ .Name }}/{{.Category}} {{ .From }} -> {{ .To }}\n{{end}}")
			tmpl.Execute(os.Stdout, changes)
			return nil
		},
	}

	err := app.Run(os.Args)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func isIn(p Package, list []Package) (bool, Package) {
	for _, pkg := range list {
		if pkg.Name == p.Name && pkg.Category == p.Category {
			return true, pkg
		}
	}
	return false, Package{}
}

type Package struct {
	Name     string `yaml:"name"`
	Category string `yaml:"category"`
	Version  string `yaml:"version,omitempty"`
}

type Changed struct {
	Name     string
	Category string
	From     string
	To       string
	New      bool
}
