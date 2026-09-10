package webui

import (
	"context"
	"embed"
	"html/template"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/kairos-io/kairos/v4/sdk/branding"
	"github.com/kairos-io/kairos/v4/sdk/constants"
	"github.com/kairos-io/kairos/v4/sdk/schema"
	"github.com/labstack/echo/v5"
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

// streamProgress serves the progress websocket: every message of the current
// install run, in order, oldest first, then each new one as it arrives.
//
// It replays from the start of the run rather than from the moment the socket
// opened, because the browser only reaches this page after the install has
// been submitted and it must survive a reload.
func streamProgress(s *state) func(c *echo.Context) error {
	return func(c *echo.Context) error {
		websocket.Handler(func(ws *websocket.Conn) {
			defer func() { _ = ws.Close() }()

			s.Lock()
			log := s.run
			s.Unlock()
			if log == nil {
				_ = websocket.JSON.Send(ws, Message{
					Type:    MessageError,
					Message: "no installation has been started",
				})
				_ = websocket.JSON.Send(ws, Message{Type: MessageDone})
				return
			}

			// The loop ends when the run is over or the client goes away,
			// and every run publishes a done message, so it cannot outlive
			// the install it is reporting on.
			idx := 0
			for {
				msgs, next, done, changed := log.since(idx)
				for _, m := range msgs {
					if err := websocket.JSON.Send(ws, m); err != nil {
						return
					}
				}
				idx = next
				if done {
					return
				}
				<-changed
			}
		}).ServeHTTP(c.Response(), c.Request())
		return nil
	}
}

// state is the one install run this server owns. A second POST to /install
// while a run is in flight, or after one that succeeded, is a reload of the
// page rather than a request to wipe the disk again.
type state struct {
	run *progressLog
	sync.Mutex
}

// Activity reports on an install started from the web UI, so a caller that
// also owns a terminal frontend can avoid tearing the server down under one.
// A nil *Activity is usable and reports no install.
type Activity struct {
	mu   sync.Mutex
	done <-chan struct{}
}

// started records the run the web UI just started, as the channel that closes
// when it ends. It must only be called once startInstall has returned nil:
// nothing publishes to a run that never started, so its channel would never
// close and WaitForInstall would block for good.
func (a *Activity) started(done <-chan struct{}) {
	if a == nil {
		return
	}
	a.mu.Lock()
	a.done = done
	a.mu.Unlock()
}

// InstallInFlight reports whether an install started from the web UI is still
// running.
func (a *Activity) InstallInFlight() bool {
	done := a.installDone()
	if done == nil {
		return false
	}
	select {
	case <-done:
		return false
	default:
		return true
	}
}

// WaitForInstall blocks until an install started from the web UI has exited,
// and returns immediately when none was ever started.
func (a *Activity) WaitForInstall() {
	if done := a.installDone(); done != nil {
		<-done
	}
}

func (a *Activity) installDone() <-chan struct{} {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.done
}

// TemplateRenderer is a custom html/template renderer for Echo framework.
type TemplateRenderer struct {
	templates *template.Template
}

// Render renders a template document.
func (t *TemplateRenderer) Render(c *echo.Context, w io.Writer, name string, data interface{}) error {
	return t.templates.ExecuteTemplate(w, name, data)
}

// Options configures a web UI server.
type Options struct {
	// Listen is the address to bind on.
	Listen string
	// Logger receives everything the server has to say: echo's startup
	// banner and port line, handler errors and the http.Server error log.
	// A nil Logger means stdout, which is what a standalone run wants.
	//
	// The installer passes a file-backed logger, because it runs the TUI on
	// the same terminal and echo's default handler writes JSON to stdout,
	// which would land on top of the alt screen.
	Logger *slog.Logger
	// Source is the install source the boot asked for, forwarded to
	// `kairos-agent manual-install --source`. Empty means the cloud-config
	// the browser submitted decides. It is the same value the TUI receives,
	// so both frontends of one installer install the same image.
	Source string
	// Activity, when non-nil, is where the server publishes the install it
	// starts, so the caller can wait for a browser-driven install to finish
	// before it shuts the server down.
	Activity *Activity
}

// StartConfigured fills in the listen address and enablement from the image's
// branding config and runs the server with the rest of o as the caller set it.
// A Listen the caller set explicitly wins over branding. It blocks until ctx
// is cancelled or the listener errors, and returns nil immediately when
// branding disabled the web UI.
func StartConfigured(ctx context.Context, o Options) error {
	agentConfig, err := branding.LoadConfig()
	if err != nil {
		return err
	}

	if agentConfig.WebUI.Disable {
		logTo(o.Logger).Info("WebUI installer disabled by branding")
		return nil
	}

	if o.Listen == "" {
		o.Listen = constants.DefaultWebUIListenAddress
		if agentConfig.WebUI.ListenAddress != "" {
			o.Listen = agentConfig.WebUI.ListenAddress
		}
	}

	return StartWith(ctx, o)
}

// StartOn runs the web UI server on the given listen address, logging to
// stdout. Tests bind on ":0" so the OS picks an ephemeral port and the run
// cannot collide with anything else listening on the developer's box.
func StartOn(ctx context.Context, listen string) error {
	return StartWith(ctx, Options{Listen: listen})
}

// logTo returns l, or a stdout logger when l is nil, so callers never have to
// nil-check before logging.
func logTo(l *slog.Logger) *slog.Logger {
	if l != nil {
		return l
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, nil))
}

// StartWith runs the web UI server and blocks until ctx is cancelled or the
// underlying listener errors.
func StartWith(ctx context.Context, o Options) error {
	sc := echo.StartConfig{
		Address:         o.Listen,
		GracefulTimeout: 10 * time.Second,
	}
	if err := sc.Start(ctx, newServer(o)); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// newServer builds the web UI's routes. It is separate from StartWith so the
// same handler can be mounted on a listener the caller owns, which is how the
// tests drive a real install over a real websocket, and how this will hang off
// the installer's own mux next to the MCP server.
func newServer(o Options) *echo.Echo {
	s := state{}

	ec := echo.NewWithConfig(echo.Config{Logger: logTo(o.Logger)})
	assetHandler := http.FileServer(getFileSystem())

	renderer := &TemplateRenderer{
		templates: template.Must(template.ParseFS(getFS(), "*.html")),
	}

	ec.Renderer = renderer

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
			// Through the logger, not stdout: in the interactive installer
			// this shares a terminal with the TUI's alt screen.
			c.Logger().Error(err.Error())
			return c.String(http.StatusOK, err.Error())
		}

		return c.String(http.StatusOK, "")
	})

	ec.POST("/install", func(c *echo.Context) error {
		formData := new(FormData)
		if err := c.Bind(formData); err != nil {
			return err
		}

		// One lock for the whole decision. Reading s.run, starting the
		// install and storing it have to be one step: two POSTs racing
		// through a check-then-act window would both start an agent and
		// two installs would partition the same disk at once. A
		// double-clicked Install button is enough to do it.
		//
		// startInstall does not block, and the goroutine it spawns
		// publishes to the log rather than touching s, so holding the lock
		// across it cannot deadlock against /ws.
		s.Lock()
		defer s.Unlock()

		// A run that is still going, or one that finished cleanly, means
		// this POST is a resubmit or a reload, not a second install. Only a
		// failed run can be retried from the form.
		if s.run != nil && (s.run.running() || s.run.succeeded()) {
			return c.Redirect(http.StatusSeeOther, "progress.html")
		}

		log := newProgressLog()

		// Report a failure to start back to the browser rather than
		// exiting. This handler shares a process with the installer TUI, so
		// bringing the process down here would take the TUI with it.
		if err := startInstall(log, o.Source, formData.CloudConfig, formData.InstallationDevice,
			finishAction(formData.Reboot, formData.PowerOff)); err != nil {
			// The run never started, so nothing will publish to it. Leave
			// s.run as it was, so the form can be submitted again and no
			// /ws connection can attach to a log nobody will write to.
			return c.Render(http.StatusOK, "message.html", map[string]interface{}{
				"message": err.Error(),
				"type":    "danger",
			})
		}
		s.run = log
		o.Activity.started(log.doneChan())

		return c.Redirect(http.StatusSeeOther, "progress.html")
	})

	ec.GET("/ws", streamProgress(&s))

	return ec
}
