/*
Copyright © 2022 SUSE LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package http

import (
	"net/http"
	"time"

	"github.com/cavaliergopher/grab/v3"
	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	"github.com/kairos-io/kairos-sdk/types/logger"
)

type Client struct {
	client *grab.Client
}

func NewClient() *Client {
	client := grab.NewClient()
	client.UserAgent = "Mozilla/5.0 (X11; Linux i686; rv:109.0) Gecko/20100101 Firefox/117.0"
	client.HTTPClient = &http.Client{Timeout: time.Second * constants.HTTPTimeout}
	return &Client{client: client}
}

// GetURL attempts to download the contents of the given URL to the given destination
func (c Client) GetURL(log logger.KairosLogger, url string, destination string) error { // nolint:revive
	req, err := grab.NewRequest(destination, url)
	if err != nil {
		log.Errorf("Failed creating a request to '%s'", url)
		return err
	}

	// start download
	log.Infof("Downloading %v...", req.URL())
	resp := c.client.Do(req)

	// start UI loop
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()

Loop:
	for {
		select {
		case <-t.C:
			log.Debugf("  transferred %v / %v bytes (%.2f%%)\n",
				resp.BytesComplete(),
				resp.Size(),
				100*resp.Progress())

		case <-resp.Done:
			// download is complete
			break Loop
		}
	}

	// check for errors
	if err := resp.Err(); err != nil {
		log.Errorf("Download failed: %v\n", err)
		return err
	}

	log.Debugf("Download saved to ./%v \n", resp.Filename)
	return nil
}
