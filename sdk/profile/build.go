package profile

import (
	"fmt"
	"os"
	"strings"

	"github.com/kairos-io/kairos-sdk/utils"
	"gopkg.in/yaml.v3"
)

type profileDataStruct struct {
	Packages []string `yaml:"packages"`
}

type profileFileStruct struct {
	Common  []string            `yaml:"common"`
	Images  []string            `yaml:"images"`
	Flavors map[string][]string `yaml:"flavors"`
}

func BuildFlavor(flavor string, profileFile string, directory string) error {
	dat, err := os.ReadFile(profileFile)

	if err != nil {
		return fmt.Errorf("error while reading profile: %w", err)
	}
	prof := &profileFileStruct{}
	if err := yaml.Unmarshal(dat, &prof); err != nil {
		return fmt.Errorf("error while unmarshalling profile: %w", err)
	}

	profiles, ok := prof.Flavors[flavor]
	if !ok {
		return fmt.Errorf("No profile found")
	}

	allPackages := []string{}
	for _, p := range profiles {
		packages, err := readProfilePackages(p, profileFile)
		if err != nil {
			return fmt.Errorf("error while reading profile: %w", err)
		}

		allPackages = append(allPackages, packages...)
	}

	if err := populateProfile(profileFile, directory, append(allPackages, prof.Common...)); err != nil {
		return fmt.Errorf("error while populating profile: %w", err)
	}

	return applyImages(directory, prof.Images)
}

func readProfilePackages(profile string, profileFile string) ([]string, error) {
	res := []string{}
	dat, err := os.ReadFile(profileFile)
	if err != nil {
		return res, fmt.Errorf("error while reading profile: %w", err)
	}

	data := map[string]interface{}{}
	prof := &profileFileStruct{}
	if err := yaml.Unmarshal(dat, &data); err != nil {
		return res, fmt.Errorf("error while unmarshalling profile: %w", err)
	}
	if err := yaml.Unmarshal(dat, &prof); err != nil {
		return res, fmt.Errorf("error while unmarshalling profile: %w", err)
	}
	p := &profileDataStruct{}
	if profileData, ok := data[profile]; ok {
		profileBlob, err := yaml.Marshal(profileData)
		if err != nil {
			return res, fmt.Errorf("error while marshalling profile: %w", err)
		}

		if err := yaml.Unmarshal(profileBlob, p); err != nil {
			return res, fmt.Errorf("error while unmarshalling profile: %w", err)
		}
		return p.Packages, nil
	}

	return res, fmt.Errorf("profile '%s' not found", profile)
}

func readProfile(profileFile string) (*profileFileStruct, error) {
	dat, err := os.ReadFile(profileFile)
	if err != nil {
		return nil, fmt.Errorf("error while reading profile: %w", err)
	}

	prof := &profileFileStruct{}

	if err := yaml.Unmarshal(dat, &prof); err != nil {
		return nil, fmt.Errorf("error while unmarshalling profile: %w", err)
	}

	return prof, nil
}

func populateProfile(config string, directory string, packages []string) error {
	cmd := fmt.Sprintf("LUET_NOLOCK=true luet install -y --config %s --system-target %s %s", config, directory, strings.Join(packages, " "))
	fmt.Println("running:", cmd)
	out, err := utils.SH(cmd)
	if err != nil {
		return fmt.Errorf("error while running luet: %w (%s)", err, out)
	}

	fmt.Println(out)
	return nil
}

func applyImages(directory string, images []string) error {
	for _, img := range images {
		cmd := fmt.Sprintf("luet util unpack %s %s", img, directory)
		fmt.Println("running:", cmd)
		out, err := utils.SH(cmd)
		if err != nil {
			return fmt.Errorf("error while running luet: %w (%s)", err, out)
		}
	}

	return nil
}

func Build(profile string, profileFile string, directory string) error {
	packages, err := readProfilePackages(profile, profileFile)
	if err != nil {
		return fmt.Errorf("error while reading profile: %w", err)
	}

	prof, err := readProfile(profileFile)
	if err != nil {
		return fmt.Errorf("error while reading profile: %w", err)
	}

	if err := populateProfile(profileFile, directory, append(packages, prof.Common...)); err != nil {
		return fmt.Errorf("error while populating profile: %w", err)
	}

	return applyImages(directory, prof.Images)
}
