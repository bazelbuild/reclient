// Copyright 2024 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package ui is used to embed the reproxy UI tar file generated from the
// reproxy_ui_tar_gen rule.
package ui

import (
	// This is necessary to use the go:embed directive below.
	_ "embed"
)

//go:embed reproxytool_ui.tar
var reproxyUITarBytes []byte

// ReproxyUITarBytes returns the embedded tar file containing the
// Angular JS app reprensenting reproxy UI.
func ReproxyUITarBytes() []byte {
	return reproxyUITarBytes
}
