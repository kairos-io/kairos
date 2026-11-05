package state

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/deniswernert/go-fstab"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("fstab entries recorded by concurrent DAG ops", func() {
	// herd runs every op of a DAG layer in its own goroutine, and
	// mount-base-overlay and custom-mount share a layer, so entries are
	// recorded concurrently. None of them may be lost.
	It("keeps every entry", func() {
		root := GinkgoT().TempDir()
		Expect(os.MkdirAll(filepath.Join(root, "etc"), 0755)).To(Succeed())
		s := &State{Rootdir: root}

		const n = 200
		var wg sync.WaitGroup
		for i := 0; i < n; i++ {
			wg.Add(2)
			go func(i int) {
				defer wg.Done()
				s.appendFstab(&fstab.Mount{Spec: fmt.Sprintf("append%d", i), File: "/a", VfsType: "none"})
			}(i)
			go func(i int) {
				defer wg.Done()
				s.AddToFstab(&fstab.Mount{Spec: fmt.Sprintf("add%d", i), File: "/b", VfsType: "none"})
			}(i)
		}
		wg.Wait()

		Expect(s.WriteFstab()(context.Background())).To(Succeed())
		out, err := os.ReadFile(filepath.Join(root, "etc", "fstab"))
		Expect(err).ToNot(HaveOccurred())
		Expect(strings.Split(strings.TrimSpace(string(out)), "\n")).To(HaveLen(2 * n))
	})
})
