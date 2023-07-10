// Copyright 2023 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package printer is responsible for printing progress of releaser.
package printer

import (
	"os"

	"github.com/fatih/color"
	log "github.com/golang/glog"
	"github.com/vardius/progress-go"
)

// StartFunc starts logging a function's progress. Returns a function
// to advance the progress bar, and a function to complete it.
func StartFunc(header string, count int) (func(int), func()) {
	Info(header)
	if count == 0 {
		return func(_ int) {}, func() {}
	}
	bar := progress.New(1, int64(count), progress.Options{
		Graph: ">",
	})
	bar.Start()
	return func(x int) {
			bar.Advance(int64(x))
		}, func() {
			if _, err := bar.Stop(); err != nil {
				log.Errorf("Failed to finish progress: %v", err)
			}
		}
}

// Fatal prints an error message to the console.
func Fatal(msg string) {
	color.Magenta("%v\n", msg)
	os.Exit(1)
}

// Error prints an error message to the console.
func Error(msg string) {
	color.Red("%v\n", msg)
}

// Warning prints a warning message to the console.
func Warning(msg string) {
	color.Yellow("%v\n", msg)
}

// Info prints an info message to the console.
func Info(msg string) {
	color.Cyan("%v\n", msg)
}

// Success prints a success message to the console.
func Success(msg string) {
	color.Green("%v\n", msg)
}
