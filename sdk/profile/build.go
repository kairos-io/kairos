package profile

import (
	"fmt"
	"io/ioutil"
	"strings"

	"github.com/kairos-io/kairos/pkg/utils"
	"gopkg.in/yaml.v3"
)

type profileDataStruct struct {
	Packages []string `yaml:"packages"`
}

type profileFileStruct struct {
	Common  []string            `yaml:"common"`
	Flavors map[string][]string `yaml:"flavors"`
}

func BuildFlavor(flavor string, profileFile string, directory string) error {
	dat, err := ioutil.ReadFile(profileFile)

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

	common, err := readCommonPackages(profileFile)
	if err != nil {
		return fmt.Errorf("error while reading common packs: %w", err)
	}
	allPackages = append(allPackages, common...)

	return populateProfile(profileFile, directory, allPackages)
}

func readProfilePackages(profile string, profileFile string) ([]string, error) {
	res := []string{}
	dat, err := ioutil.ReadFile(profileFile)
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

func readCommonPackages(profileFile string) ([]string, error) {
	res := []string{}
	dat, err := ioutil.ReadFile(profileFile)
	if err != nil {
		return res, fmt.Errorf("error while reading profile: %w", err)
	}

	prof := &profileFileStruct{}

	if err := yaml.Unmarshal(dat, &prof); err != nil {
		return res, fmt.Errorf("error while unmarshalling profile: %w", err)
	}

	return prof.Common, nil
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

func Build(profile string, profileFile string, directory string) error {
	packages, err := readProfilePackages(profile, profileFile)
	if err != nil {
		return fmt.Errorf("error while reading profile: %w", err)
	}

	common, err := readCommonPackages(profileFile)
	if err != nil {
		return fmt.Errorf("error while reading profile: %w", err)
	}

	return populateProfile(profileFile, directory, append(packages, common...))
}
