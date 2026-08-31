/*
Copyright © 2022 - 2023 SUSE LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package platform

import (
	"fmt"
	"strings"

	registry "github.com/google/go-containerregistry/pkg/v1"
	"gopkg.in/yaml.v3"
)

const (
	ArchAmd64 = "amd64"
	Archx86   = "x86_64"
	ArchArm64 = "arm64"
	ArchRiscv = "riscv64"
)

type Platform struct {
	OS         string
	Arch       string
	GolangArch string
}

func NewPlatform(os, arch string) (*Platform, error) {
	golangArch, err := archToGolangArch(arch)
	if err != nil {
		return nil, err
	}

	arch, err = golangArchToArch(arch)
	if err != nil {
		return nil, err
	}

	return &Platform{
		OS:         os,
		Arch:       arch,
		GolangArch: golangArch,
	}, nil
}

func NewPlatformFromArch(arch string) (*Platform, error) {
	return NewPlatform("linux", arch)
}

func ParsePlatform(platform string) (*Platform, error) {
	p, err := registry.ParsePlatform(platform)
	if err != nil {
		return nil, err
	}

	return NewPlatform(p.OS, p.Architecture)
}

func (p *Platform) updateFrom(platform *Platform) {
	if platform == nil || p == nil {
		return
	}

	p.OS = platform.OS
	p.Arch = platform.Arch
	p.GolangArch = platform.GolangArch
}

func (p *Platform) String() string {
	if p == nil {
		return ""
	}

	return fmt.Sprintf("%s/%s", p.OS, p.GolangArch)
}

func (p Platform) MarshalYAML() (interface{}, error) {
	return p.String(), nil
}

func (p *Platform) UnmarshalYAML(value *yaml.Node) error {
	parsed, err := ParsePlatform(value.Value)
	if err != nil {
		return err
	}
	p.updateFrom(parsed)
	return nil
}

func (p *Platform) CustomUnmarshal(data interface{}) (bool, error) {
	str, ok := data.(string)
	if !ok {
		return false, fmt.Errorf("can't unmarshal %+v to a Platform type", data)
	}

	parsed, err := ParsePlatform(str)
	p.updateFrom(parsed)
	return false, err
}

var errInvalidArch = fmt.Errorf("invalid arch")

func archToGolangArch(arch string) (string, error) {
	switch strings.ToLower(arch) {
	case ArchAmd64:
		return ArchAmd64, nil
	case Archx86:
		return ArchAmd64, nil
	case ArchArm64:
		return ArchArm64, nil
	case ArchRiscv:
		return ArchRiscv, nil
	default:
		return "", errInvalidArch
	}
}

func golangArchToArch(arch string) (string, error) {
	switch strings.ToLower(arch) {
	case Archx86:
		return Archx86, nil
	case ArchAmd64:
		return Archx86, nil
	case ArchArm64:
		return ArchArm64, nil
	case ArchRiscv:
		return ArchRiscv, nil
	default:
		return "", errInvalidArch
	}
}
