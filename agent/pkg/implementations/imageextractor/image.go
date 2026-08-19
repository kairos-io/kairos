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

package imageextractor

import (
	image "github.com/kairos-io/kairos-sdk/utils/image"
)

// OCIImageExtractor is the default ImageExtractor implementation. It now lives in
// the kairos-sdk (utils/image) so it is the single source of truth shared across
// kairos-agent and AuroraBoot. This alias keeps the historical import path working.
//
// Set Insecure to allow pulling from registries served over plain HTTP or
// presenting an untrusted/self-signed TLS certificate.
type OCIImageExtractor = image.OCIImageExtractor
