package webui

import (
	"context"
	"embed"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"sync"
	"text/template"
	"time"

	"github.com/kairos-io/kairos/v2/internal/agent"
	"github.com/kairos-io/kairos/v2/pkg/config"
	"github.com/labstack/echo/v4"
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

func getFS() fs.FS {
	fsys, err := fs.Sub(embededFiles, "public")
	if err != nil {
		panic(err)
	}

	return fsys
}

func streamProcess(s *state) func(c echo.Context) error {
	return func(c echo.Context) error {
		consumeError := func(err error) {
			if err != nil {
				c.Logger().Error(err)
			}
		}
		websocket.Handler(func(ws *websocket.Conn) {
			defer ws.Close()
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
					if err == nil {
						err := websocket.Message.Send(ws, string(errOut))
						consumeError(err)
					}
					out, err := os.ReadFile(s.p.StdoutPath())
					if err == nil {
						err = websocket.Message.Send(ws, string(out))
						consumeError(err)
					}
					err = websocket.Message.Send(ws, "Process stopped!")
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

				for {
					select {
					case line := <-t.Lines:
						err = websocket.Message.Send(ws, line.Text+"\r\n")
						consumeError(err)
					case line := <-t2.Lines:
						err = websocket.Message.Send(ws, line.Text+"\r\n")
						consumeError(err)
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
func (t *TemplateRenderer) Render(w io.Writer, name string, data interface{}, c echo.Context) error {

	// Add global methods if data is a map
	if viewContext, isMap := data.(map[string]interface{}); isMap {
		viewContext["reverse"] = c.Echo().Reverse
	}

	return t.templates.ExecuteTemplate(w, name, data)
}

func Start(ctx context.Context) error {

	s := state{}
	listen := config.DefaultWebUIListenAddress

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

	ec.POST("/validate", func(c echo.Context) error {
		formData := new(FormData)
		if err := c.Bind(formData); err != nil {
			return err
		}
		cloudConfig := formData.CloudConfig

		err := agent.Validate(cloudConfig)
		if err != nil {
			return c.String(http.StatusOK, err.Error())
		}

		return c.String(http.StatusOK, "")
	})

	ec.POST("/install", func(c echo.Context) error {

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

	if err := ec.Start(listen); err != nil && err != http.ErrServerClosed {
		return err
	}

	go func() {
		<-ctx.Done()
		ct, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := ec.Shutdown(ct)
		if err != nil {
			log.Printf("shutdown failed: %s", err.Error())
		}
		cancel()
	}()

	return nil
}
