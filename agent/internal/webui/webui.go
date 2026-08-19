package webui

import (
	"context"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/kairos-io/kairos-agent/v2/internal/agent"
	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	"github.com/kairos-io/kairos-sdk/schema"
	"github.com/labstack/echo/v5"
	process "github.com/mudler/go-processmanager"
	"github.com/nxadm/tail"
	"golang.org/x/net/websocket"
)

type FormData struct {
	CloudConfig string `form:"cloud-config" json:"cloud-config" query:"cloud-config"`
	Reboot      string `form:"reboot" json:"reboot" query:"reboot"`

	PowerOff           string `form:"power-off" json:"power-off" query:"power-off"`
	InstallationDevice string `form:"installation-device" json:"installation-device" query:"installation-device"`
}

//go:embed public
var embededFiles embed.FS

func getFileSystem() http.FileSystem {
	fsys, err := fs.Sub(embededFiles, "public")
	if err != nil {
		panic(err)
	}

	return http.FS(fsys)
}

// GetFileSystem returns the embedded file system for testing purposes
func GetFileSystem() http.FileSystem {
	return getFileSystem()
}

func getFS() fs.FS {
	fsys, err := fs.Sub(embededFiles, "public")
	if err != nil {
		panic(err)
	}

	return fsys
}

// ansiToHTML converts ANSI escape codes to HTML spans with CSS colors
var ansiRegex = regexp.MustCompile(`\x1b\[([0-9;]*)m`)

func ansiToHTML(s string) string {
	// Map ANSI color codes to CSS colors (optimized for dark backgrounds)
	colorMap := map[int]string{
		30: "#6272a4", // Black -> Gray (better visibility on dark bg)
		31: "#ff5555", // Red
		32: "#50fa7b", // Green
		33: "#f1fa8c", // Yellow
		34: "#bd93f9", // Blue
		35: "#ff79c6", // Magenta
		36: "#8be9fd", // Cyan
		37: "#f8f8f2", // White
		90: "#6272a4", // Bright Black (Gray)
		91: "#ff6e6e", // Bright Red
		92: "#69ff94", // Bright Green
		93: "#ffffa5", // Bright Yellow
		94: "#d6acff", // Bright Blue
		95: "#ff92d0", // Bright Magenta
		96: "#a4ffff", // Bright Cyan
		97: "#ffffff", // Bright White
	}

	// Parse ANSI codes and convert to HTML
	result := ""
	lastIndex := 0
	currentColor := ""
	currentBold := false

	matches := ansiRegex.FindAllStringSubmatchIndex(s, -1)

	// If no ANSI codes, just escape and return
	if len(matches) == 0 {
		text := s
		text = strings.ReplaceAll(text, "&", "&amp;")
		text = strings.ReplaceAll(text, "<", "&lt;")
		text = strings.ReplaceAll(text, ">", "&gt;")
		return text
	}

	for _, match := range matches {
		// Add text before the ANSI code
		if match[0] > lastIndex {
			text := s[lastIndex:match[0]]
			// Escape HTML
			text = strings.ReplaceAll(text, "&", "&amp;")
			text = strings.ReplaceAll(text, "<", "&lt;")
			text = strings.ReplaceAll(text, ">", "&gt;")

			if currentColor != "" || currentBold {
				style := ""
				if currentBold {
					style += "font-weight: bold; "
				}
				if currentColor != "" {
					style += "color: " + currentColor + "; "
				}
				result += fmt.Sprintf("<span style=\"%s\">%s</span>", style, text)
			} else {
				result += text
			}
		}

		// Parse the ANSI code
		codeStr := s[match[2]:match[3]]
		codes := strings.Split(codeStr, ";")

		for _, code := range codes {
			if code == "" {
				code = "0"
			}
			var codeNum int
			fmt.Sscanf(code, "%d", &codeNum)

			switch {
			case codeNum == 0:
				// Reset
				currentColor = ""
				currentBold = false
			case codeNum == 1:
				// Bold
				currentBold = true
			case codeNum >= 30 && codeNum <= 37:
				// Foreground color
				currentColor = colorMap[codeNum]
			case codeNum >= 90 && codeNum <= 97:
				// Bright foreground color
				currentColor = colorMap[codeNum]
			}
		}

		lastIndex = match[1]
	}

	// Add remaining text
	if lastIndex < len(s) {
		text := s[lastIndex:]
		// Escape HTML
		text = strings.ReplaceAll(text, "&", "&amp;")
		text = strings.ReplaceAll(text, "<", "&lt;")
		text = strings.ReplaceAll(text, ">", "&gt;")

		if currentColor != "" || currentBold {
			style := ""
			if currentBold {
				style += "font-weight: bold; "
			}
			if currentColor != "" {
				style += "color: " + currentColor + "; "
			}
			result += fmt.Sprintf("<span style=\"%s\">%s</span>", style, text)
		} else {
			result += text
		}
	}

	return result
}

// formatLogLine formats a log line to be more readable and converts ANSI to HTML
func formatLogLine(line string) string {
	// Remove leading/trailing whitespace
	line = strings.TrimSpace(line)

	// Skip empty lines
	if line == "" {
		return ""
	}

	// Convert ANSI codes to HTML - this preserves colors and formatting
	return ansiToHTML(line)
}

// stripANSI removes ANSI escape codes (used for parsing only)
func stripANSI(s string) string {
	return ansiRegex.ReplaceAllString(s, "")
}

func streamProcess(s *state) func(c *echo.Context) error {
	return func(c *echo.Context) error {
		consumeError := func(err error) {
			if err != nil {
				c.Logger().Error(err.Error())
			}
		}
		websocket.Handler(func(ws *websocket.Conn) {
			defer func() { _ = ws.Close() }()
			for {
				s.Lock()
				if s.p == nil {
					// Write
					err := websocket.Message.Send(ws, "No process!")
					consumeError(err)
					s.Unlock()
					return
				}
				s.Unlock()

				if !s.p.IsAlive() {
					errOut, err := os.ReadFile(s.p.StderrPath())
					if err == nil && len(errOut) > 0 {
						lines := strings.Split(string(errOut), "\n")
						for _, line := range lines {
							formatted := formatLogLine(line)
							if formatted != "" {
								err := websocket.Message.Send(ws, formatted+"\n")
								consumeError(err)
							}
						}
					}
					out, err := os.ReadFile(s.p.StdoutPath())
					if err == nil && len(out) > 0 {
						lines := strings.Split(string(out), "\n")
						for _, line := range lines {
							formatted := formatLogLine(line)
							if formatted != "" {
								err := websocket.Message.Send(ws, formatted+"\n")
								consumeError(err)
							}
						}
					}
					err = websocket.Message.Send(ws, "[INFO] Process stopped!\n")
					consumeError(err)
					// Send completion signal so frontend can show download button
					err = websocket.Message.Send(ws, "[COMPLETE] Installation finished\n")
					consumeError(err)
					return
				}

				t, err := tail.TailFile(s.p.StdoutPath(), tail.Config{Follow: true})
				if err != nil {
					return
				}
				t2, err := tail.TailFile(s.p.StderrPath(), tail.Config{Follow: true})
				if err != nil {
					return
				}

				completionSent := false
				for {
					// Check if process finished
					s.Lock()
					processAlive := s.p != nil && s.p.IsAlive()
					s.Unlock()

					if !processAlive && !completionSent {
						// Process finished, send completion message
						err = websocket.Message.Send(ws, "[COMPLETE] Installation finished\n")
						consumeError(err)
						completionSent = true
						// Continue reading remaining logs for a bit, then exit
						time.Sleep(2 * time.Second)
						return
					}

					select {
					case line := <-t.Lines:
						if line == nil {
							// File closed
							return
						}
						formatted := formatLogLine(line.Text)
						if formatted != "" {
							err = websocket.Message.Send(ws, formatted+"\n")
							consumeError(err)
						}
					case line := <-t2.Lines:
						if line == nil {
							// File closed
							return
						}
						formatted := formatLogLine(line.Text)
						if formatted != "" {
							err = websocket.Message.Send(ws, formatted+"\n")
							consumeError(err)
						}
					case <-time.After(1 * time.Second):
						// Periodic check - continue loop to check process status
						continue
					}
				}
			}
		}).ServeHTTP(c.Response(), c.Request())
		return nil
	}
}

type state struct {
	p *process.Process
	sync.Mutex
}

// TemplateRenderer is a custom html/template renderer for Echo framework.
type TemplateRenderer struct {
	templates *template.Template
}

// Render renders a template document.
func (t *TemplateRenderer) Render(c *echo.Context, w io.Writer, name string, data interface{}) error {
	return t.templates.ExecuteTemplate(w, name, data)
}

func Start(ctx context.Context) error {

	s := state{}
	listen := constants.DefaultWebUIListenAddress

	ec := echo.New()
	assetHandler := http.FileServer(getFileSystem())

	renderer := &TemplateRenderer{
		templates: template.Must(template.ParseFS(getFS(), "*.html")),
	}

	ec.Renderer = renderer
	agentConfig, err := agent.LoadConfig()
	if err != nil {
		return err
	}

	if agentConfig.WebUI.ListenAddress != "" {
		listen = agentConfig.WebUI.ListenAddress
	}

	if agentConfig.WebUI.Disable {
		log.Println("WebUI installer disabled by branding")
		return nil
	}

	ec.GET("/*", echo.WrapHandler(http.StripPrefix("/", assetHandler)))

	ec.POST("/validate", func(c *echo.Context) error {
		formData := new(FormData)
		if err := c.Bind(formData); err != nil {
			return err
		}
		cloudConfig := formData.CloudConfig

		// Use the same validation approach as the rest of the codebase
		// which understands Kairos-specific structures like users in stages
		err := schema.Validate(cloudConfig)
		if err != nil {
			fmt.Printf("Validation error: %s", err.Error())
			return c.String(http.StatusOK, err.Error())
		}

		return c.String(http.StatusOK, "")
	})

	ec.POST("/install", func(c *echo.Context) error {

		s.Lock()
		if s.p != nil {
			status, _ := s.p.ExitCode()
			if s.p.IsAlive() || status == "0" {
				s.Unlock()
				return c.Redirect(http.StatusSeeOther, "progress.html")
			}
		}
		s.Unlock()

		formData := new(FormData)
		if err := c.Bind(formData); err != nil {
			return err
		}

		// Process the form data as necessary
		cloudConfig := formData.CloudConfig
		reboot := formData.Reboot
		powerOff := formData.PowerOff
		installationDevice := formData.InstallationDevice

		args := []string{"manual-install"}

		if powerOff == "on" {
			args = append(args, "--poweroff")
		}
		if reboot == "on" {
			args = append(args, "--reboot")
		}
		args = append(args, "--device", installationDevice)

		// create tempfile to store cloud-config, bail out if we fail as we couldn't go much further
		file, err := os.CreateTemp("", "install-webui-*.yaml")
		if err != nil {
			log.Fatalf("could not create tmpfile for cloud-config: %s", err.Error())
		}

		err = os.WriteFile(file.Name(), []byte(cloudConfig), 0600)
		if err != nil {
			log.Fatalf("could not write tmpfile for cloud-config: %s", err.Error())
		}

		args = append(args, file.Name())

		s.Lock()
		s.p = process.New(process.WithName("/usr/bin/kairos-agent"), process.WithArgs(args...), process.WithTemporaryStateDir())
		s.Unlock()
		err = s.p.Run()
		if err != nil {
			return c.Render(http.StatusOK, "message.html", map[string]interface{}{
				"message": err.Error(),
				"type":    "danger",
			})
		}

		// Start install process, lock with sentinel
		return c.Redirect(http.StatusSeeOther, "progress.html")
	})

	ec.GET("/ws", streamProcess(&s))

	sc := echo.StartConfig{
		Address:         listen,
		GracefulTimeout: 10 * time.Second,
	}
	if err := sc.Start(ctx, ec); err != nil && err != http.ErrServerClosed {
		return err
	}

	return nil
}
