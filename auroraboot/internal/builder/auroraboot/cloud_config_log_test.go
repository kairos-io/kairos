package auroraboot_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/AuroraBoot/internal/builder/auroraboot"
	"github.com/kairos-io/AuroraBoot/pkg/builder"
	"github.com/kairos-io/AuroraBoot/pkg/store"
)

// chunkCollector is a LogBroadcaster that keeps every chunk it is sent.
type chunkCollector struct {
	mu     sync.Mutex
	chunks []string
}

func (c *chunkCollector) BroadcastLogChunk(_ string, chunk string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.chunks = append(c.chunks, chunk)
}

func (c *chunkCollector) all() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return strings.Join(c.chunks, "")
}

var _ = Describe("AuroraBoot Builder cloud-config in the build log", func() {
	const marker = "marker-secret-7f3a9c1e5b"

	cloudConfig := "#cloud-config\nk3s:\n  args:\n  - --token=" + marker + "\n"

	BeforeEach(func() {
		// The build derives its image with docker; a stub on PATH lets the
		// build reach the deployer without a docker daemon.
		bin := GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(bin, "docker"), []byte("#!/bin/sh\nexit 0\n"), 0o755)).To(Succeed())
		GinkgoT().Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	})

	It("logs the size of the cloud-config, never its contents or a digest of them", func() {
		s := newRecStore()
		chunks := &chunkCollector{}
		b := auroraboot.New(GinkgoT().TempDir(), noopDeploy, s).WithLogBroadcaster(chunks)

		_, err := b.Build(context.Background(), builder.BuildOptions{
			ID:          "with-cloud-config",
			BaseImage:   "quay.io/kairos/hadron:latest",
			ISO:         true,
			CloudConfig: cloudConfig,
		})
		Expect(err).NotTo(HaveOccurred())

		Eventually(func() string {
			r, err := s.GetByID(context.Background(), "with-cloud-config")
			if err != nil {
				return ""
			}
			return r.Phase
		}, 10*time.Second).Should(Equal(store.ArtifactReady))

		want := fmt.Sprintf("Cloud config: %d bytes\n", len(cloudConfig))
		// A digest of a mostly templated document would let a log reader
		// test guesses for the secrets in it.
		sum := sha256.Sum256([]byte(cloudConfig))
		digest := hex.EncodeToString(sum[:])

		stored, err := s.GetLogs(context.Background(), "with-cloud-config")
		Expect(err).NotTo(HaveOccurred())
		Expect(stored).To(ContainSubstring(want))
		Expect(stored).NotTo(ContainSubstring(marker))
		Expect(stored).NotTo(ContainSubstring(digest))

		streamed := chunks.all()
		Expect(streamed).To(ContainSubstring(want))
		Expect(streamed).NotTo(ContainSubstring(marker))
		Expect(streamed).NotTo(ContainSubstring(digest))
	})
})
