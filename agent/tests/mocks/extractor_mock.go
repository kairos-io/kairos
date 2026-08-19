/*
Copyright © 2022 - 2023 SUSE LLC

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
	sdkImages "github.com/kairos-io/kairos-sdk/types/images"
	sdkLogger "github.com/kairos-io/kairos-sdk/types/logger"
)

type FakeImageExtractor struct {
	Logger         sdkLogger.KairosLogger
	SideEffect     func(imageRef, destination, platformRef string) error
	SizeSideEffect func(imageRef, platformRef string) (int64, error)
	ClientCalls    []ExtractCall
}

type ExtractCall struct {
	ImageRef    string
	Destination string
	PlatformRef string
}

func (f *FakeImageExtractor) GetOCIImageSize(imageRef, platformRef string) (int64, error) {
	if f.SizeSideEffect != nil {
		return f.SizeSideEffect(imageRef, platformRef)
	}
	return 0, nil
}

var _ sdkImages.ImageExtractor = &FakeImageExtractor{}

func NewFakeImageExtractor(logger sdkLogger.KairosLogger) *FakeImageExtractor {
	l := logger
	if &l == nil {
		logger = sdkLogger.NewNullLogger()
	}
	return &FakeImageExtractor{
		Logger: logger,
	}
}

func (f *FakeImageExtractor) ExtractImage(imageRef, destination, platformRef string, excludes ...string) error {
	f.Logger.Debugf("extracting %s to %s in platform %s", imageRef, destination, platformRef)
	f.ClientCalls = append(f.ClientCalls, ExtractCall{ImageRef: imageRef, Destination: destination, PlatformRef: platformRef})
	if f.SideEffect != nil {
		f.Logger.Debugf("running side effect")
		return f.SideEffect(imageRef, destination, platformRef)
	}

	return nil
}

// WasCalledWithImageRef is a helper method to confirm that the client was called with the given image ref
func (f *FakeImageExtractor) WasCalledWithImageRef(imageRef string) bool {
	for _, c := range f.ClientCalls {
		if c.ImageRef == imageRef {
			return true
		}
	}
	return false
}

// WasCalledWithExtractCall is a helper method to confirm that the client was called with the given extract call
// This matches exactly the calls made to the client in all fields
func (f *FakeImageExtractor) WasCalledWithExtractCall(call ExtractCall) bool {
	for _, c := range f.ClientCalls {
		if c == call {
			return true
		}
	}
	return false
}
