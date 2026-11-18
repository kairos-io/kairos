package handlers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/AuroraBoot/pkg/handlers"
	"github.com/kairos-io/AuroraBoot/pkg/store"
	"github.com/labstack/echo/v4"
)

var _ = Describe("ArtifactHandler: removing local build images", func() {
	var (
		e         *echo.Echo
		handler   *handlers.ArtifactHandler
		dockerLog string
	)

	// removedImages returns the images the fake docker was asked to rmi.
	removedImages := func() []string {
		data, err := os.ReadFile(dockerLog)
		if os.IsNotExist(err) {
			return nil
		}
		Expect(err).NotTo(HaveOccurred())
		var images []string
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			if rest, ok := strings.CutPrefix(line, "rmi -f "); ok {
				images = append(images, rest)
			}
		}
		return images
	}

	BeforeEach(func() {
		bin := GinkgoT().TempDir()
		dockerLog = filepath.Join(bin, "docker.log")
		script := "#!/bin/sh\necho \"$*\" >> " + dockerLog + "\n"
		Expect(os.WriteFile(filepath.Join(bin, "docker"), []byte(script), 0o755)).To(Succeed())
		GinkgoT().Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

		e = echo.New()
		as := &fakeArtifactStore{}
		for _, r := range []*store.ArtifactRecord{
			{ID: "ready-a", Phase: store.ArtifactReady, ContainerImage: "auroraboot-kairos:ready-a"},
			{ID: "fail-b", Phase: store.ArtifactError},
		} {
			Expect(as.Create(context.Background(), r)).To(Succeed())
		}
		handler = handlers.NewArtifactHandler(&fakeBuilder{}, as, nil, nil, nil, nil, GinkgoT().TempDir(), "reg-token", "http://localhost:8080")
	})

	deleteArtifact := func(id string) {
		rec := httptest.NewRecorder()
		c := e.NewContext(httptest.NewRequest(http.MethodDelete, "/api/v1/artifacts/"+id, nil), rec)
		c.SetParamNames("id")
		c.SetParamValues(id)
		Expect(handler.Delete(c)).To(Succeed())
		Expect(rec.Code).To(Equal(http.StatusNoContent))
	}

	It("removes both build tags when a Ready artifact is deleted", func() {
		deleteArtifact("ready-a")
		Expect(removedImages()).To(ContainElements("auroraboot-build:ready-a", "auroraboot-kairos:ready-a"))
	})

	It("removes both build tags when a failed artifact with no ContainerImage is deleted", func() {
		deleteArtifact("fail-b")
		Expect(removedImages()).To(ContainElements("auroraboot-build:fail-b", "auroraboot-kairos:fail-b"))
	})

	It("removes both build tags of every failed artifact on clear failed", func() {
		rec := httptest.NewRecorder()
		c := e.NewContext(httptest.NewRequest(http.MethodDelete, "/api/v1/artifacts/failed", nil), rec)
		Expect(handler.ClearFailed(c)).To(Succeed())
		Expect(rec.Code).To(Equal(http.StatusNoContent))

		images := removedImages()
		Expect(images).To(ContainElements("auroraboot-build:fail-b", "auroraboot-kairos:fail-b"))
		Expect(images).NotTo(ContainElement(HaveSuffix(":ready-a")))
	})
})
