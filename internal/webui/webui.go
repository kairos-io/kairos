package webui

import (
	"context"
	"embed"
	"io/fs"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/kairos-io/kairos/internal/agent"
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

func streamProcess(s *state) func(c echo.Context) error {
	return func(c echo.Context) error {
		websocket.Handler(func(ws *websocket.Conn) {
			defer ws.Close()
			for {
				s.Lock()
				if s.p == nil {
					// Write
					err := websocket.Message.Send(ws, "No process!")
					if err != nil {
						c.Logger().Error(err)
					}
					s.Unlock()
					return
				}
				s.Unlock()

				if !s.p.IsAlive() {
					err := websocket.Message.Send(ws, "Process stopped!")
					if err != nil {
						c.Logger().Error(err)
					}
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
						websocket.Message.Send(ws, line.Text)
					case line := <-t2.Lines:
						websocket.Message.Send(ws, line.Text)
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

func Start(ctx context.Context, l string) error {

	s := state{}
	ec := echo.New()
	assetHandler := http.FileServer(getFileSystem())

	agentConfig, err := agent.LoadConfig()
	if err != nil {
		return err
	}
	if agentConfig.DisableWebUIInstall {
		log.Println("WebUI installer disabled by branding")
		return nil
	}

	ec.GET("/*", echo.WrapHandler(http.StripPrefix("/", assetHandler)))

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

		if powerOff == "poweroff" {
			args = append(args, "--poweroff")
		}
		if reboot == "reboot" {
			args = append(args, "--reboot")
		}
		args = append(args, "--device", installationDevice)

		file, err := ioutil.TempFile("", "install-webui")
		if err != nil {
			log.Fatal(err)
		}

		os.WriteFile(file.Name(), []byte(cloudConfig), 0600)
		args = append(args, file.Name())

		s.Lock()
		s.p = process.New(process.WithName("/usr/bin/kairos-agent"), process.WithArgs(args...), process.WithTemporaryStateDir())
		s.Unlock()
		err = s.p.Run()
		if err != nil {
			log.Println(err)
		}

		// Start install process, lock with sentinel
		return c.Redirect(http.StatusSeeOther, "progress.html")
	})

	ec.GET("/ws", streamProcess(&s))

	if err := ec.Start(l); err != nil && err != http.ErrServerClosed {
		return err
	}

	go func() {
		<-ctx.Done()
		ct, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		ec.Shutdown(ct)
		cancel()
	}()

	return nil
}
