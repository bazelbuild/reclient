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

// Package logrecordserver implements a local server with an Angular app
// that can be used for debugging a specific RRPL file.
package logrecordserver

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/bazelbuild/reclient/internal/pkg/logger"
	"github.com/gorilla/mux"

	lpb "github.com/bazelbuild/reclient/api/log"
	log "github.com/golang/glog"
)

// Server encapsulates the HTTP server.
type Server struct {
	records        []*lpb.LogRecord
	jsonLogRecords []byte

	loadComplete bool
	loadErr      error
	loadLock     sync.RWMutex

	loadRecordsOnce sync.Once
}

// LoadLogRecords loads all the log records into memory
// from the given logPath file.
func (lr *Server) LoadLogRecords(logPath string) {
	lr.loadRecordsOnce.Do(func() {
		log.Infof("Loading log record file %v...", logPath)
		format, fp, err := logger.ParseFilepath(logPath)
		if err != nil {
			lr.setErr(err)
			return
		}

		lr.records, err = logger.ParseFromFile(format, fp)
		if err != nil {
			lr.setErr(err)
			return
		}
		logDump := &lpb.LogDump{
			Records: lr.records,
		}

		log.Infof("Converting log records to JSON...")
		lr.jsonLogRecords, err = json.Marshal(logDump.GetRecords())
		if err != nil {
			lr.setErr(err)
			return
		}
		lr.setLoadComplete()
		log.Infof("Finished loading log records...")
	})
	return
}

func (lr *Server) logRecords(w http.ResponseWriter, r *http.Request) {
	if !lr.loaded() && lr.loadError() == nil {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("Still loading log records..."))
		return
	}

	log.V(1).Infof("Received request to get API data...")
	if lr.loadError() != nil {
		http.Error(w, lr.loadError().Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Write(lr.jsonLogRecords)
}

func (lr *Server) setErr(err error) {
	lr.loadLock.Lock()
	defer lr.loadLock.Unlock()
	lr.loadErr = err
}

func (lr *Server) setLoadComplete() {
	lr.loadLock.Lock()
	defer lr.loadLock.Unlock()
	lr.loadComplete = true
}

func (lr *Server) loaded() bool {
	lr.loadLock.RLock()
	defer lr.loadLock.RUnlock()
	return lr.loadComplete
}

func (lr *Server) loadError() error {
	lr.loadLock.RLock()
	defer lr.loadLock.RUnlock()
	return lr.loadErr
}

// Start starts up the log records server.
func Start(lr *Server) {
	r := mux.NewRouter()
	r.HandleFunc("/api/data", lr.logRecords).Methods("GET")
	http.ListenAndServe("localhost:9080", r)
}
