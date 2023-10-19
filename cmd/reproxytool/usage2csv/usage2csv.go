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
//
// Package usage2CSV is used to parse the usage data from reproxy.INFO log file
// to a CSV file, ordered by timestamp. The output csv file will simply append a
// csv suffix after the full name of the input reproxy.INFO file.
//
// To quickly view the csv as a PNG once done, you can use the csv tools inside
// of G3's contentads/brand/trends/tool directory from your cider v terminal.
// For example, the following cmd will remote the title row from the csv,
// transpose the csv and then plot it as a PNG.
//
//sed -i '1d' /tmp/reproxy.INFO.csv \
//&& blaze run //contentads/brand/trends/tool/csv_transpose -- \
//   --input_file=/tmp/reproxy.INFO.csv --output_file=/tmp/reproxy.INFO.csv \
//&& blaze run //contentads/brand/trends/tool/csv_plot -- \
//   --input_file=/tmp/reproxy.INFO.csv --output_file=/tmp/output.png
//
// For more info about this tool,
// see: https://g3doc.corp.google.com/contentads/brand/trends/tool/csv_plot/README.md

package usage2csv

import (
	"bufio"
	"encoding/csv"
	"io"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	log "github.com/golang/glog"
)

var (
	usages     = []*usage{}
	usageRegex = regexp.MustCompile(`Resource Usage: map\[(.*?)\]`)
)

type usage struct {
	timestamp, CPUPct, MemResMbs, MemVirtMbs, MemPct, PeakNumActioins int64
}

func unmarshall(s string) *usage {
	var u usage
	fields := strings.Split(s, " ")
	for _, field := range fields {
		kv := strings.Split(field, ":")
		key := kv[0]
		value, err := strconv.ParseInt(kv[1], 10, 64)
		if err != nil {
			log.Fatalf("Cannot unmarshall %v from %s to int64: %v", kv[1], s, err)
			return nil
		}
		switch key {
		case "UNIX_TIME":
			u.timestamp = value
		case "CPU_pct":
			u.CPUPct = value
		case "MEM_RES_mbs":
			u.MemResMbs = value
		case "MEM_VIRT_mbs":
			u.MemVirtMbs = value
		case "MEM_pct":
			u.MemPct = value
		case "PEAK_NUM_ACTIOINS":
			u.PeakNumActioins = value
		}
	}
	return &u
}

func sortByTimestamp() {
	sort.Slice(usages, func(i, j int) bool {
		return usages[i].timestamp < usages[j].timestamp
	})
}

func saveToCSV(logPath string) error {
	filePath := logPath + ".csv"
	f, err := os.Create(filePath)
	if err != nil {
		return err
	}
	log.Infof("CSV file created at: %v", filePath)
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	if err := w.Write([]string{
		"timestamp",
		"CPU_pct",
		"MEM_RES_mbs",
		"MEM_VIRT_mbs",
		"MEM_pct",
		"PEAK_NUM_ACTIOINS",
	}); err != nil {
		return err
	}

	for _, u := range usages {
		if err := w.Write([]string{
			strconv.FormatInt(u.timestamp, 10),
			strconv.FormatInt(u.CPUPct, 10),
			strconv.FormatInt(u.MemResMbs, 10),
			strconv.FormatInt(u.MemVirtMbs, 10),
			strconv.FormatInt(u.MemPct, 10),
			strconv.FormatInt(u.PeakNumActioins, 10),
		}); err != nil {
			return err
		}
	}
	return nil
}

func parseLogFileToSlice(file io.Reader) {
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		l := scanner.Text()
		// Extract data from "Resource Usage: map[CPU_pct:0 MEM_RES_mbs:1246 MEM_VIRT_mbs:5895 MEM_pct:0 PEAK_NUM_ACTIOINS:0 UNIX_TIME:1697736889]".
		dataMatches := usageRegex.FindStringSubmatch(l)
		if dataMatches == nil || len(dataMatches) != 2 {
			continue
		}
		if usg := unmarshall(dataMatches[1]); usg != nil {
			usages = append(usages, usg)
		}
	}
}

// Usage2CSV reads a reproxy.INFO file at logPath, and parse the resource usage
// content as a CSV file. The CSV file will have the same name as the logPath
// but append with a ".csv" suffix.
func Usage2CSV(logPath string) error {
	file, err := os.Open(logPath)
	if err != nil {
		log.Fatalf("Cannot open reproxy.INFO file at %v : %v", logPath, err)
	}
	defer file.Close()
	parseLogFileToSlice(file)
	sortByTimestamp()
	return saveToCSV(logPath)
}
