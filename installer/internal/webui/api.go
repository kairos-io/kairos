package webui

import (
	"context"
	"net/http"
	"sync"

	"github.com/labstack/echo/v5"

	"github.com/kairos-io/kairos/v4/sdk/schema"

	"github.com/kairos-io/kairos/v4/installer/internal/wizard"
)

// wizardAPI serves the steps and folds answers for the browser. The browser
// keeps the answers between calls, so the only state here is the resolved
// steps, which Apply validates against.
type wizardAPI struct {
	env    wizard.Env
	source string

	mu    sync.Mutex
	steps []wizard.Step
}

func (w *wizardAPI) refresh(ctx context.Context) []wizard.Step {
	steps := wizard.Steps(ctx, w.env)
	w.mu.Lock()
	w.steps = steps
	w.mu.Unlock()
	return steps
}

func (w *wizardAPI) current(ctx context.Context) []wizard.Step {
	w.mu.Lock()
	steps := w.steps
	w.mu.Unlock()
	if steps == nil {
		return w.refresh(ctx)
	}
	return steps
}

// errorKey is the JSON key the page reads a failure from.
const errorKey = "error"

type stepRequest struct {
	Answers wizard.Answers    `json:"answers"`
	Values  map[string]string `json:"values"`
}

type stepResponse struct {
	Answers wizard.Answers      `json:"answers"`
	Errors  []wizard.FieldError `json:"errors"`
}

func (w *wizardAPI) register(ec *echo.Echo) {
	ec.GET("/api/wizard", func(c *echo.Context) error {
		// advanced_disabled is the branding switch that hides the terminal
		// installer's editor; the page makes its review read only with it.
		return c.JSON(http.StatusOK, map[string]any{
			"steps":             w.refresh(c.Request().Context()),
			"advanced_disabled": w.env.AdvancedDisabled(),
		})
	})

	ec.POST("/api/step/:id", func(c *echo.Context) error {
		var req stepRequest
		if err := c.Bind(&req); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{errorKey: err.Error()})
		}
		req.Answers.Source = w.source
		answers, errs := wizard.Apply(w.current(c.Request().Context()), req.Answers, c.Param("id"), req.Values)
		if errs == nil {
			errs = []wizard.FieldError{}
		}
		return c.JSON(http.StatusOK, stepResponse{Answers: answers, Errors: errs})
	})

	// The wizard page checks the cloud-config on the review step. The message
	// is the schema's own; an empty one means the document is valid.
	ec.POST("/validate-json", func(c *echo.Context) error {
		var req struct {
			CloudConfig string `json:"cloud_config"`
		}
		if err := c.Bind(&req); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{errorKey: err.Error()})
		}
		msg := ""
		if err := schema.Validate(req.CloudConfig); err != nil {
			msg = err.Error()
		}
		return c.JSON(http.StatusOK, map[string]string{errorKey: msg})
	})

	ec.POST("/api/render", func(c *echo.Context) error {
		var req stepRequest
		if err := c.Bind(&req); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{errorKey: err.Error()})
		}
		req.Answers.Source = w.source
		out, err := wizard.Render(req.Answers)
		if err != nil {
			return c.JSON(http.StatusUnprocessableEntity, map[string]string{errorKey: err.Error()})
		}
		return c.JSON(http.StatusOK, map[string]string{"cloud_config": out})
	})
}
