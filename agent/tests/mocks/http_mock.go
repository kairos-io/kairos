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

package mocks

import (
	"errors"

	sdkLogger "github.com/kairos-io/kairos-sdk/types/logger"
)

// FakeHTTPClient is an implementation of HTTPClient interface used for testing
// It stores Get calls into ClientCalls for easy checking of what was called
type FakeHTTPClient struct {
	ClientCalls []string
	Error       bool
}

// GetURL will return a FakeHttpBody and store the url call into ClientCalls
func (m *FakeHTTPClient) GetURL(_ sdkLogger.KairosLogger, url string, destination string) error {
	// Store calls to the mock client, so we can verify that we didnt mangled them or anything
	m.ClientCalls = append(m.ClientCalls, url)
	if m.Error {
		return errors.New("fake http error")
	}
	return nil
}

// WasGetCalledWith is a helper method to confirm that the client wazs called with the give url
func (m *FakeHTTPClient) WasGetCalledWith(url string) bool {
	for _, c := range m.ClientCalls {
		if c == url {
			return true
		}
	}
	return false
}
