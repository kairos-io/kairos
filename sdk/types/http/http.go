package http

import "github.com/kairos-io/kairos/v4/sdk/types/logger"

type Client interface {
	GetURL(log logger.KairosLogger, url string, destination string) error
}
