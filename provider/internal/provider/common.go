package provider

import (
	"fmt"

	"github.com/mudler/go-pluggable"
)

func ErrorEvent(format string, a ...interface{}) pluggable.EventResponse {
	return pluggable.EventResponse{Error: fmt.Sprintf(format, a...)}
}
