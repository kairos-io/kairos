package versioneer

import (
	"fmt"

	"github.com/urfave/cli/v2"
)

var (
	flavorFlag *cli.StringFlag = &cli.StringFlag{
		Name:    "flavor",
		Value:   "",
		Usage:   "the OS flavor (e.g. opensuse)",
		EnvVars: []string{EnvVarFlavor},
	}

	flavorReleaseFlag *cli.StringFlag = &cli.StringFlag{
		Name:    "flavor-release",
		Value:   "",
		Usage:   "the OS flavor release (e.g. leap-15.5)",
		EnvVars: []string{EnvVarFlavorRelease},
	}

	variantFlag *cli.StringFlag = &cli.StringFlag{
		Name:    "variant",
		Value:   "",
		Usage:   "the Kairos variant (core, standard)",
		EnvVars: []string{EnvVarVariant},
	}

	modelFlag *cli.StringFlag = &cli.StringFlag{
		Name:    "model",
		Value:   "",
		Usage:   "the model for which the OS was built (e.g. rpi4)",
		EnvVars: []string{EnvVarModel},
	}

	archFlag *cli.StringFlag = &cli.StringFlag{
		Name:    "arch",
		Value:   "",
		Usage:   "the architecture of the OS",
		EnvVars: []string{EnvVarArch},
	}

	versionFlag *cli.StringFlag = &cli.StringFlag{
		Name:    "version",
		Value:   "",
		Usage:   "the Kairos version (e.g. v2.4.2)",
		EnvVars: []string{EnvVarVersion},
	}

	softwareVersionFlag *cli.StringFlag = &cli.StringFlag{
		Name:    "software-version",
		Value:   "",
		Usage:   "the software version (e.g. k3sv1.28.2+k3s1)",
		EnvVars: []string{EnvVarSoftwareVersion},
	}

	softwareVersionPrefixFlag *cli.StringFlag = &cli.StringFlag{
		Name:    "software-version-prefix",
		Value:   "",
		Usage:   "the string that separates the Kairos version from the software version (e.g. \"k3s\")",
		EnvVars: []string{EnvVarSoftwareVersionPrefix},
	}

	registryAndOrgFlag *cli.StringFlag = &cli.StringFlag{
		Name:    "registry-and-org",
		Value:   "",
		Usage:   "the container registry and org (e.g. \"quay.io/kairos\")",
		EnvVars: []string{EnvVarRegistryAndOrg},
	}

	idFlag *cli.StringFlag = &cli.StringFlag{
		Name:    "id",
		Value:   "",
		Usage:   "a identifier for the artifact (e.g. \"master\")",
		EnvVars: []string{EnvVarID},
	}

	githubRepoFlag *cli.StringFlag = &cli.StringFlag{
		Name:    "github-repo",
		Value:   "",
		Usage:   "the Github repository where the code is hosted",
		EnvVars: []string{EnvVarGithubRepo},
	}

	bugReportURLFlag *cli.StringFlag = &cli.StringFlag{
		Name:    "bug-report-url",
		Value:   "",
		Usage:   "the url where bugs can be reported",
		EnvVars: []string{EnvVarBugReportURL},
	}

	projectHomeURLFlag *cli.StringFlag = &cli.StringFlag{
		Name:    "project-home-url",
		Value:   "",
		Usage:   "the url where more information about the project can be found",
		EnvVars: []string{EnvVarHomeURL},
	}

	familyFlag *cli.StringFlag = &cli.StringFlag{
		Name:    "family",
		Value:   "",
		Usage:   "family of the underlying distro (rhel, ubuntu, opensuse, etc...)",
		EnvVars: []string{EnvVarFamily},
	}
)

func CliCommands() []*cli.Command {
	return []*cli.Command{
		{
			Name:  "container-artifact-name",
			Usage: "generates an artifact name for Kairos OCI images",
			Flags: []cli.Flag{
				flavorFlag, flavorReleaseFlag, variantFlag, modelFlag, archFlag,
				versionFlag, softwareVersionFlag, softwareVersionPrefixFlag, registryAndOrgFlag,
			},
			Action: func(cCtx *cli.Context) error {
				a := artifactFromFlags(cCtx)

				result, err := a.ContainerName(cCtx.String(registryAndOrgFlag.Name))
				if err != nil {
					return err
				}
				fmt.Println(result)

				return nil
			},
		},
		{
			Name:  "bootable-artifact-name",
			Usage: "generates a name for bootable artifacts (e.g. iso files)",
			Flags: []cli.Flag{
				flavorFlag, flavorReleaseFlag, variantFlag, modelFlag, archFlag,
				versionFlag, softwareVersionFlag, softwareVersionPrefixFlag,
			},
			Action: func(cCtx *cli.Context) error {
				a := artifactFromFlags(cCtx)

				result, err := a.BootableName()
				if err != nil {
					return err
				}
				fmt.Println(result)

				return nil
			},
		},
		{
			Name:  "base-container-artifact-name",
			Usage: "generates a name for base (not yet Kairos) images",
			Flags: []cli.Flag{
				flavorFlag, flavorReleaseFlag, variantFlag, modelFlag, archFlag,
				registryAndOrgFlag, idFlag,
			},
			Action: func(cCtx *cli.Context) error {
				a := artifactFromFlags(cCtx)

				result, err := a.BaseContainerName(
					cCtx.String(registryAndOrgFlag.Name), cCtx.String(idFlag.Name))
				if err != nil {
					return err
				}
				fmt.Println(result)

				return nil
			},
		},
		{
			Name:  "os-release-variables",
			Usage: "generates a set of variables to be appended in the /etc/kairos-release file",
			Flags: []cli.Flag{
				flavorFlag, flavorReleaseFlag, variantFlag, modelFlag, archFlag, versionFlag,
				softwareVersionFlag, softwareVersionPrefixFlag, registryAndOrgFlag, bugReportURLFlag, projectHomeURLFlag,
				githubRepoFlag, familyFlag,
			},
			Action: func(cCtx *cli.Context) error {
				a := artifactFromFlags(cCtx)

				result, err := a.OSReleaseVariables(
					registryAndOrgFlag.Get(cCtx),
					githubRepoFlag.Get(cCtx),
					bugReportURLFlag.Get(cCtx),
					projectHomeURLFlag.Get(cCtx),
				)
				if err != nil {
					return err
				}
				// Do a normal print as we are already adding a \n at the end in the OSReleaseVariables function
				// to avoid getting 1 empty line
				fmt.Print(result)

				return nil
			},
		},
	}
}

func artifactFromFlags(cCtx *cli.Context) Artifact {
	return Artifact{
		Flavor:                flavorFlag.Get(cCtx),
		Family:                familyFlag.Get(cCtx),
		FlavorRelease:         flavorReleaseFlag.Get(cCtx),
		Variant:               variantFlag.Get(cCtx),
		Model:                 modelFlag.Get(cCtx),
		Arch:                  archFlag.Get(cCtx),
		Version:               versionFlag.Get(cCtx),
		SoftwareVersion:       softwareVersionFlag.Get(cCtx),
		SoftwareVersionPrefix: softwareVersionPrefixFlag.Get(cCtx),
	}
}
