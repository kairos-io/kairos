// Copyright © 2022 Ettore Di Giacinto <mudler@c3os.io>
//
// This program is free software; you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation; either version 2 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License along
// with this program; if not, see <http://www.gnu.org/licenses/>.

package config_test

import (
	"os"

	// . "github.com/kairos-io/kairos/v2/pkg/config"
	. "github.com/onsi/ginkgo/v2"
	// . "github.com/onsi/gomega"
)

type TConfig struct {
	Kairos struct {
		OtherKey     string `yaml:"other_key"`
		NetworkToken string `yaml:"network_token"`
	} `yaml:"kairos"`
}

var _ = Describe("Config", func() {
	var d string
	BeforeEach(func() {
		d, _ = os.MkdirTemp("", "xxxx")
	})

	AfterEach(func() {
		if d != "" {
			os.RemoveAll(d)
		}
	})
})
