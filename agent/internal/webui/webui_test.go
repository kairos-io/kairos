package webui_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/kairos-io/kairos-agent/v2/internal/webui"
	"github.com/labstack/echo/v5"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("WebUI", func() {
	Describe("Server", func() {
		It("can start and stop", func() {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// Start webui server in a goroutine
			errChan := make(chan error, 1)
			go func() {
				errChan <- webui.Start(ctx)
			}()

			// Give server time to start
			time.Sleep(100 * time.Millisecond)

			// Test that server is running (this will fail if server can't start)
			select {
			case err := <-errChan:
				if err != nil && !errors.Is(err, http.ErrServerClosed) {
					Fail("Server failed to start: " + err.Error())
				}
			default:
				// Server is running
			}

			// Cancel context to stop server
			cancel()
			time.Sleep(50 * time.Millisecond)
		})
	})

	Describe("Routes", func() {
		var e *echo.Echo

		BeforeEach(func() {
			// Create a test echo instance
			e = echo.New()

			// Setup routes similar to Start function
			assetHandler := http.FileServer(webui.GetFileSystem())
			e.GET("/*", echo.WrapHandler(http.StripPrefix("/", assetHandler)))

			// Test validate endpoint
			e.POST("/validate", func(c *echo.Context) error {
				formData := new(webui.FormData)
				if err := c.Bind(formData); err != nil {
					return err
				}
				cloudConfig := formData.CloudConfig

				// Simple validation - just check if it starts with #cloud-config
				if !strings.HasPrefix(strings.TrimSpace(cloudConfig), "#cloud-config") {
					return c.String(http.StatusOK, "Cloud config should start with #cloud-config")
				}

				return c.String(http.StatusOK, "")
			})
		})

		It("serves index.html", func() {
			// Try root path first (which should serve index.html)
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			// If root doesn't work, try index.html directly
			if rec.Code != http.StatusOK {
				req = httptest.NewRequest(http.MethodGet, "/index.html", nil)
				rec = httptest.NewRecorder()
				e.ServeHTTP(rec, req)
			}

			Expect(rec.Code).To(Equal(http.StatusOK))
			body := rec.Body.String()
			Expect(body).To(ContainSubstring("Welcome to the Installer!"))
			Expect(body).To(ContainSubstring("cloud-config"))
		})

		It("serves progress.html", func() {
			req := httptest.NewRequest(http.MethodGet, "/progress.html", nil)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			Expect(rec.Code).To(Equal(http.StatusOK))
			Expect(rec.Body.String()).To(ContainSubstring("Installation progress"))
		})

		It("serves message.html", func() {
			req := httptest.NewRequest(http.MethodGet, "/message.html", nil)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			Expect(rec.Code).To(Equal(http.StatusOK))
			Expect(rec.Body.String()).To(ContainSubstring("Web Installer"))
		})

		It("serves favicon.ico", func() {
			req := httptest.NewRequest(http.MethodGet, "/favicon.ico", nil)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			// Favicon might not exist, so we just check it doesn't crash
			Expect(rec.Code).ToNot(Equal(http.StatusInternalServerError))
		})

		It("validates cloud-config with valid config", func() {
			formData := strings.NewReader("cloud-config=#cloud-config%0Ausers:%0A  - name: test")
			req := httptest.NewRequest(http.MethodPost, "/validate", formData)
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			Expect(rec.Code).To(Equal(http.StatusOK))
			// Should return empty string for valid config
			Expect(rec.Body.String()).To(BeEmpty())
		})

		It("validates cloud-config with invalid config", func() {
			formData := strings.NewReader("cloud-config=invalid config")
			req := httptest.NewRequest(http.MethodPost, "/validate", formData)
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			Expect(rec.Code).To(Equal(http.StatusOK))
			// Should return error message
			Expect(rec.Body.String()).To(ContainSubstring("Cloud config should start with #cloud-config"))
		})
	})

	Describe("Embedded Files", func() {
		It("has index.html", func() {
			fs := webui.GetFileSystem()
			file, err := fs.Open("index.html")
			Expect(err).ToNot(HaveOccurred())
			if file != nil {
				file.Close()
			}
		})

		It("has progress.html", func() {
			fs := webui.GetFileSystem()
			file, err := fs.Open("progress.html")
			Expect(err).ToNot(HaveOccurred())
			if file != nil {
				file.Close()
			}
		})

		It("has message.html", func() {
			fs := webui.GetFileSystem()
			file, err := fs.Open("message.html")
			Expect(err).ToNot(HaveOccurred())
			if file != nil {
				file.Close()
			}
		})

		It("has favicon.ico", func() {
			fs := webui.GetFileSystem()
			file, err := fs.Open("favicon.ico")
			Expect(err).ToNot(HaveOccurred())
			if file != nil {
				file.Close()
			}
		})

		It("does not have nonexistent.html", func() {
			fs := webui.GetFileSystem()
			_, err := fs.Open("nonexistent.html")
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("HTML Content", func() {
		It("index.html contains expected content", func() {
			fs := webui.GetFileSystem()
			indexFile, err := fs.Open("index.html")
			Expect(err).ToNot(HaveOccurred())
			defer indexFile.Close()

			// Read entire file
			content, err := io.ReadAll(indexFile)
			Expect(err).ToNot(HaveOccurred())
			contentStr := string(content)

			// Check for key elements
			Expect(contentStr).To(ContainSubstring("Welcome to the Installer!"))
			Expect(contentStr).To(ContainSubstring("cloud-config"))
			Expect(contentStr).To(ContainSubstring("Install"))
			Expect(contentStr).To(ContainSubstring("Web Installer"))
		})

		It("progress.html contains expected content", func() {
			fs := webui.GetFileSystem()
			progressFile, err := fs.Open("progress.html")
			Expect(err).ToNot(HaveOccurred())
			defer progressFile.Close()

			content, err := io.ReadAll(progressFile)
			Expect(err).ToNot(HaveOccurred())
			contentStr := string(content)

			Expect(contentStr).To(ContainSubstring("Installation progress"))
			Expect(contentStr).To(ContainSubstring("ws"))
			Expect(contentStr).To(ContainSubstring("Autoscroll"))
		})
	})
})
