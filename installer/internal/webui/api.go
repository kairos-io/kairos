package webui

import (
	"context"
	"net/http"
	"sync"

	"github.com/labstack/echo/v5"

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
		return c.JSON(http.StatusOK, map[string]any{"steps": w.refresh(c.Request().Context())})
	})

	ec.POST("/api/step/:id", func(c *echo.Context) error {
		var req stepRequest
		if err := c.Bind(&req); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
		req.Answers.Source = w.source
		answers, errs := wizard.Apply(w.current(c.Request().Context()), req.Answers, c.Param("id"), req.Values)
		if errs == nil {
			errs = []wizard.FieldError{}
		}
		return c.JSON(http.StatusOK, stepResponse{Answers: answers, Errors: errs})
	})

	ec.POST("/api/render", func(c *echo.Context) error {
		var req stepRequest
		if err := c.Bind(&req); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
		req.Answers.Source = w.source
		out, err := wizard.Render(req.Answers)
		if err != nil {
			return c.JSON(http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		}
		return c.JSON(http.StatusOK, map[string]string{"cloud_config": out})
	})
}
