package main

import (
	"context"
	"os"
	"path/filepath"

	"github.com/kairos-io/kairos/v4/pkg/testartifacts"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("test-artifacts", func() {
	ctx := context.Background()

	It("fails without a subcommand", func() {
		Expect(run(ctx, nil)).To(MatchError(ContainSubstring("usage")))
	})

	It("fails on an unknown subcommand", func() {
		Expect(run(ctx, []string{"bogus"})).To(MatchError(ContainSubstring("unknown subcommand \"bogus\"")))
	})

	It("requires --out", func() {
		Expect(run(ctx, []string{"keys"})).To(MatchError(ContainSubstring("--out is required")))
	})

	It("requires --name for an extension", func() {
		Expect(run(ctx, []string{"sysext", "--out", GinkgoT().TempDir()})).To(MatchError(ContainSubstring("--name is required")))
		Expect(run(ctx, []string{"sysext-plain", "--out", GinkgoT().TempDir()})).To(MatchError(ContainSubstring("--name is required")))
	})

	It("rejects an unknown architecture", func() {
		err := run(ctx, []string{"sysext", "--out", GinkgoT().TempDir(), "--name", "work", "--arch", "riscv"})
		Expect(err).To(MatchError(ContainSubstring("--arch must be amd64 or arm64")))
	})

	It("rejects stray arguments", func() {
		Expect(run(ctx, []string{"sysext", "--out", GinkgoT().TempDir(), "--name", "work", "extra", "--arch", "arm64"})).To(MatchError(ContainSubstring("unexpected argument \"extra\"")))
	})

	It("converts relative paths to absolute", func() {
		wd, err := os.Getwd()
		Expect(err).NotTo(HaveOccurred())
		var relPath = "relative/path"
		var emptyPath = ""
		var absPath string

		err = absPaths(&relPath, &emptyPath, &absPath)
		Expect(err).NotTo(HaveOccurred())

		Expect(relPath).To(Equal(filepath.Join(wd, "relative/path")))
		Expect(emptyPath).To(Equal(""))
		Expect(absPath).To(Equal(""))
	})

	It("generates a key set", Label("docker"), func() {
		if !testartifacts.DockerAvailable() {
			Fail("this spec runs AuroraBoot with Docker, and no Docker daemon is reachable")
		}
		out := GinkgoT().TempDir()
		Expect(run(ctx, []string{"keys", "--out", out})).To(Succeed())
		Expect(filepath.Join(out, "db.key")).To(BeARegularFile())
		Expect(filepath.Join(out, "tpm2-pcr-private.pem")).To(BeARegularFile())
	})
})
