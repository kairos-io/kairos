package http

import "github.com/kairos-io/kairos-sdk/types/logger"

type Client interface {
	GetURL(log logger.KairosLogger, url string, destination string) error
}
