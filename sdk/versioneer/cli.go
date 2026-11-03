package versioneer

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// cliValues holds the values of the versioneer flags of one command.
type cliValues struct {
	artifact       Artifact
	registryAndOrg string
	id             string
	githubRepo     string
	bugReportURL   string
	projectHomeURL string
}

// cliFlag describes one versioneer flag: the environment variable that gives
// its default, its usage text, and the value it sets.
type cliFlag struct {
	env   string
	usage string
	value func(*cliValues) *string
}

var cliFlags = map[string]cliFlag{
	"flavor": {EnvVarFlavor, "the OS flavor (e.g. opensuse)",
		func(v *cliValues) *string { return &v.artifact.Flavor }},
	"flavor-release": {EnvVarFlavorRelease, "the OS flavor release (e.g. leap-15.5)",
		func(v *cliValues) *string { return &v.artifact.FlavorRelease }},
	"variant": {EnvVarVariant, "the Kairos variant (core, standard)",
		func(v *cliValues) *string { return &v.artifact.Variant }},
	"model": {EnvVarModel, "the model for which the OS was built (e.g. rpi4)",
		func(v *cliValues) *string { return &v.artifact.Model }},
	"arch": {EnvVarArch, "the architecture of the OS",
		func(v *cliValues) *string { return &v.artifact.Arch }},
	"version": {EnvVarVersion, "the Kairos version (e.g. v2.4.2)",
		func(v *cliValues) *string { return &v.artifact.Version }},
	"software-version": {EnvVarSoftwareVersion, "the software version (e.g. k3sv1.28.2+k3s1)",
		func(v *cliValues) *string { return &v.artifact.SoftwareVersion }},
	"software-version-prefix": {EnvVarSoftwareVersionPrefix, "the string that separates the Kairos version from the software version (e.g. \"k3s\")",
		func(v *cliValues) *string { return &v.artifact.SoftwareVersionPrefix }},
	"registry-and-org": {EnvVarRegistryAndOrg, "the container registry and org (e.g. \"quay.io/kairos\")",
		func(v *cliValues) *string { return &v.registryAndOrg }},
	"id": {EnvVarID, "a identifier for the artifact (e.g. \"master\")",
		func(v *cliValues) *string { return &v.id }},
	"github-repo": {EnvVarGithubRepo, "the Github repository where the code is hosted",
		func(v *cliValues) *string { return &v.githubRepo }},
	"bug-report-url": {EnvVarBugReportURL, "the url where bugs can be reported",
		func(v *cliValues) *string { return &v.bugReportURL }},
	"project-home-url": {EnvVarHomeURL, "the url where more information about the project can be found",
		func(v *cliValues) *string { return &v.projectHomeURL }},
	"family": {EnvVarFamily, "family of the underlying distro (rhel, ubuntu, opensuse, etc...)",
		func(v *cliValues) *string { return &v.artifact.Family }},
}

// addFlags registers the named flags on flags. Each one defaults to its
// environment variable, so the command line wins over the environment.
func (v *cliValues) addFlags(flags *pflag.FlagSet, names ...string) {
	for _, name := range names {
		f := cliFlags[name]
		flags.StringVar(f.value(v), name, os.Getenv(f.env), fmt.Sprintf("%s [$%s]", f.usage, f.env))
	}
}

// CliCommands returns the versioneer commands, for the standalone versioneer
// binary and for `kairos-agent versioneer`.
func CliCommands() []*cobra.Command {
	var container cliValues
	containerCmd := &cobra.Command{
		Use:   "container-artifact-name",
		Short: "generates an artifact name for Kairos OCI images",
		RunE: func(*cobra.Command, []string) error {
			result, err := container.artifact.ContainerName(container.registryAndOrg)
			if err != nil {
				return err
			}
			fmt.Println(result)

			return nil
		},
	}
	container.addFlags(containerCmd.Flags(),
		"flavor", "flavor-release", "variant", "model", "arch",
		"version", "software-version", "software-version-prefix", "registry-and-org")

	var bootable cliValues
	bootableCmd := &cobra.Command{
		Use:   "bootable-artifact-name",
		Short: "generates a name for bootable artifacts (e.g. iso files)",
		RunE: func(*cobra.Command, []string) error {
			result, err := bootable.artifact.BootableName()
			if err != nil {
				return err
			}
			fmt.Println(result)

			return nil
		},
	}
	bootable.addFlags(bootableCmd.Flags(),
		"flavor", "flavor-release", "variant", "model", "arch",
		"version", "software-version", "software-version-prefix")

	var base cliValues
	baseCmd := &cobra.Command{
		Use:   "base-container-artifact-name",
		Short: "generates a name for base (not yet Kairos) images",
		RunE: func(*cobra.Command, []string) error {
			result, err := base.artifact.BaseContainerName(base.registryAndOrg, base.id)
			if err != nil {
				return err
			}
			fmt.Println(result)

			return nil
		},
	}
	base.addFlags(baseCmd.Flags(),
		"flavor", "flavor-release", "variant", "model", "arch",
		"registry-and-org", "id")

	var osRelease cliValues
	osReleaseCmd := &cobra.Command{
		Use:   "os-release-variables",
		Short: "generates a set of variables to be appended in the /etc/kairos-release file",
		RunE: func(*cobra.Command, []string) error {
			result, err := osRelease.artifact.OSReleaseVariables(
				osRelease.registryAndOrg,
				osRelease.githubRepo,
				osRelease.bugReportURL,
				osRelease.projectHomeURL,
			)
			if err != nil {
				return err
			}
			// Do a normal print as we are already adding a \n at the end in the OSReleaseVariables function
			// to avoid getting 1 empty line
			fmt.Print(result)

			return nil
		},
	}
	osRelease.addFlags(osReleaseCmd.Flags(),
		"flavor", "flavor-release", "variant", "model", "arch", "version",
		"software-version", "software-version-prefix", "registry-and-org", "bug-report-url", "project-home-url",
		"github-repo", "family")

	return []*cobra.Command{containerCmd, bootableCmd, baseCmd, osReleaseCmd}
}
