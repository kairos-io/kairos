/*
Copyright © 2026 Kairos authors

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

package k8s_test

import (
	"os"

	"github.com/kairos-io/kairos-agent/v2/pkg/utils/k8s"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("GetHostDirForK8s", Label("k8s"), func() {
	// saveAndUnset stores the current value of an env var and unsets it, then
	// registers a DeferCleanup to restore the original state (set or unset).
	saveAndUnset := func(key string) {
		orig, present := os.LookupEnv(key)
		Expect(os.Unsetenv(key)).To(Succeed())
		DeferCleanup(func() {
			if present {
				Expect(os.Setenv(key, orig)).To(Succeed())
			} else {
				Expect(os.Unsetenv(key)).To(Succeed())
			}
		})
	}

	BeforeEach(func() {
		// Ensure both env vars start from a known (unset) state and are
		// restored after each spec, so tests do not leak env vars.
		saveAndUnset("KUBERNETES_SERVICE_HOST")
		saveAndUnset("HOST_DIR")
	})

	When("not running under kubernetes", func() {
		It("returns an empty string", func() {
			Expect(k8s.GetHostDirForK8s()).To(Equal(""))
		})

		It("returns an empty string even if HOST_DIR is set", func() {
			Expect(os.Setenv("HOST_DIR", "/somewhere")).To(Succeed())
			Expect(k8s.GetHostDirForK8s()).To(Equal(""))
		})
	})

	When("running under kubernetes", func() {
		BeforeEach(func() {
			Expect(os.Setenv("KUBERNETES_SERVICE_HOST", "10.0.0.1")).To(Succeed())
		})

		It("returns HOST_DIR when it is set", func() {
			Expect(os.Setenv("HOST_DIR", "/custom-host")).To(Succeed())
			Expect(k8s.GetHostDirForK8s()).To(Equal("/custom-host"))
		})

		It("defaults to /host when HOST_DIR is empty", func() {
			Expect(k8s.GetHostDirForK8s()).To(Equal("/host"))
		})

		It("defaults to /host when HOST_DIR is set but empty", func() {
			Expect(os.Setenv("HOST_DIR", "")).To(Succeed())
			Expect(k8s.GetHostDirForK8s()).To(Equal("/host"))
		})
	})
})
