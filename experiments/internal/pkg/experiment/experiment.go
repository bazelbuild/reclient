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

// Package experiment is responsible for running experiments on GCE VMs.
package experiment

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"team/foundry-x/re-client/experiments/internal/pkg/gcs"
	"team/foundry-x/re-client/experiments/internal/pkg/vm"

	epb "team/foundry-x/re-client/experiments/api/experiment"

	"google.golang.org/protobuf/proto"

	log "github.com/golang/glog"
	"golang.org/x/sync/errgroup"

	"github.com/bazelbuild/rules_go/go/tools/bazel"
)

const (
	logFile     = "/tmp/exp.log"
	timeFile    = "/tmp/time.txt"
	trialFile   = "/tmp/trial.txt"
	outDir      = "outputs"
	reclientDir = "reclient"
)

var (
	reclientBinaries = []string{
		"cmd/bootstrap",
		"cmd/dumpstats",
		"cmd/reproxy",
		"cmd/rewrapper",
	}
)

// Experiment encapsulates information about an experiment and logic to run it.
type Experiment struct {
	date         string
	resBucket    string
	gcpProject   string
	expPb        *epb.Experiment
	baseDir      string
	vms          map[string]*vm.VM
	tmpDir       map[string]string
	runConfigs   map[string]*epb.RunConfiguration
	setupScripts map[string]string
	buildScripts map[string]string
	logFrequency int
}

// NewExperiment creates a new experiment using the experiment proto.
func NewExperiment(expPb *epb.Experiment, baseDir string, gcpProject, date, resBucket string, logFrequency int) (*Experiment, error) {
	if err := verify(expPb); err != nil {
		return nil, fmt.Errorf("Experiment proto invalid: %v", err)
	}
	return &Experiment{
		expPb:        expPb,
		baseDir:      baseDir,
		gcpProject:   gcpProject,
		date:         date,
		resBucket:    resBucket,
		setupScripts: map[string]string{},
		buildScripts: map[string]string{},
		logFrequency: logFrequency,
	}, nil
}

func verify(expPb *epb.Experiment) error {
	// TODO: Check RC Names are unique
	// TODO: Check that you are using the reclient setup command
	return nil
}

func (e *Experiment) tryDownloadLogFiles(ctx context.Context) error {
	return e.runOnConfigs(func(rc *epb.RunConfiguration) error {
		v := e.vms[rc.GetName()]
		log.Infof("Downloading exp.log from %v", v.Name())
		v.CopyFilesFromVM(ctx, logFile, filepath.Join(e.tmpDir[rc.GetName()], "exp.log"))
		return nil
	})
}

// Run runs the experiment.
func (e *Experiment) Run(ctx context.Context) (err error) {
	defer func() {
		if err == nil {
			e.cleanup(ctx)
		} else {
			log.Errorf("Experiment failed! Resources were not cleaned for investigation. Please cleanup resources manually.")
			log.Errorf(e.debugMessage(ctx))
			e.tryDownloadLogFiles(ctx)
		}
	}()

	if err := e.prepRCs(ctx); err != nil {
		return err
	}
	if err := e.startVMs(ctx); err != nil {
		return err
	}
	if err := e.copyInputs(ctx); err != nil {
		return err
	}
	if err := e.downloadReclient(ctx); err != nil {
		return err
	}
	if err := e.runSetup(ctx); err != nil {
		return err
	}
	for i := 0; i < int(e.expPb.NumTrials); i++ {
		log.Infof("Starting Trial %v", i)
		if err := e.runBuilds(ctx); err != nil {
			return err
		}
		if err := e.downloadOutputs(ctx, i); err != nil {
			return err
		}
		if err := e.tearDown(ctx); err != nil {
			return err
		}
		if err := e.cleanOutDirs(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (e *Experiment) cleanup(ctx context.Context) {
	g := new(errgroup.Group)
	for _, v := range e.vms {
		v := v
		g.Go(func() error {
			_, err := v.Delete(ctx, false)
			return err
		})
	}
	g.Wait()

	for _, d := range e.tmpDir {
		os.RemoveAll(d)
	}
}

func (e *Experiment) cleanOutDirs(ctx context.Context) error {
	for _, d := range e.tmpDir {
		od := filepath.Join(d, outDir)
		if err := os.RemoveAll(od); err != nil {
			return err
		}
		// Recreate the directory for future outputs.
		if err := os.Mkdir(od, 0777); err != nil {
			return err
		}
	}
	return nil
}

func (e *Experiment) prepRCs(ctx context.Context) error {
	e.tmpDir = make(map[string]string)
	e.runConfigs = make(map[string]*epb.RunConfiguration)
	for _, rc := range e.expPb.GetRunConfigurations() {
		if rc.NumMachines == 0 {
			rc.NumMachines = 1
		}

		for i := uint32(0); i < rc.GetNumMachines(); i++ {
			dest, err := os.MkdirTemp("", e.expPb.GetName())
			if err != nil {
				return err
			}

			rcCopy := proto.Clone(rc).(*epb.RunConfiguration)
			rcCopy.Name += fmt.Sprintf("-%d", i)

			rc := applyConfig(e.expPb.GetBaseConfiguration(), rcCopy)
			e.runConfigs[rc.GetName()] = rc
			e.tmpDir[rc.GetName()] = dest
			if err := os.Mkdir(filepath.Join(e.tmpDir[rc.GetName()], outDir), 0777); err != nil {
				return err
			}
			if err := os.Mkdir(filepath.Join(e.tmpDir[rc.GetName()], reclientDir), 0777); err != nil {
				return err
			}
		}
	}
	return nil
}

func (e *Experiment) debugMessage(ctx context.Context) string {
	debugMsg := ""
	delCmds := ""

	for name, vm := range e.vms {
		tmp, ok := e.tmpDir[name]
		if !ok {
			tmp = "MISSING_TMP_DIR_NAME"
		}

		debugMsg += fmt.Sprintf("Experiment %s ran at VM %s. Local temp folder at %s.\n", name, vm.Name(), tmp)
		vmDelCommand, _ := vm.Delete(ctx, true)
		delCmds += fmt.Sprintf("$ %s\n", vmDelCommand)
	}
	debugMsg += fmt.Sprintf("There may be partial results at gs://%v/%v_%v/\n", e.resBucket, e.expPb.GetName(), e.date)
	debugMsg += fmt.Sprintf("Delete the VMs running:\n")
	debugMsg += delCmds

	return debugMsg
}

func (e *Experiment) runOnConfigs(f func(rc *epb.RunConfiguration) error) error {
	g := new(errgroup.Group)
	for _, rc := range e.runConfigs {
		rc := rc
		g.Go(func() error {
			return f(rc)
		})
	}
	return g.Wait()
}

func (e *Experiment) startVMs(ctx context.Context) error {
	e.vms = make(map[string]*vm.VM)
	var lock sync.Mutex
	return e.runOnConfigs(func(rc *epb.RunConfiguration) error {
		v := vm.NewMachine(e.rcID(rc), e.gcpProject, rc.GetMachineSettings())
		if v == nil {
			log.Fatal("Could not create machine based on configuration settings")
		}
		lock.Lock()
		e.vms[rc.GetName()] = v
		lock.Unlock()
		if err := v.CreateWithDisk(ctx); err != nil {
			return err
		}
		if err := v.Mount(ctx); err != nil {
			return err
		}
		return nil
	})
}

func (e *Experiment) copyInputs(ctx context.Context) error {
	return e.runOnConfigs(func(rc *epb.RunConfiguration) error {
		v, ok := e.vms[rc.GetName()]
		if !ok {
			return fmt.Errorf("could not find VM for run configuration %v", rc.GetName())
		}

		for _, input := range rc.GetInputs() {
			src := filepath.Join(e.baseDir, input.Source)

			if input.GetVmDirect() {
				if err := v.CopyFilesToVM(ctx, src, input.Destination); err != nil {
					return err
				}
			} else {
				base := filepath.Base(input.Destination)
				gcsDest := fmt.Sprintf("gs://%v/%v_%v/%v/inputs/%v", e.resBucket, e.expPb.GetName(), e.date, rc.GetName(), base)
				gcs.Copy(ctx, src, gcsDest)

				var ok bool
				var err error
				// gsutil sometimes is reported as non-existent - no idea why that happens.
				// Heuristically, retrying fixes this problem.
				for i := 0; i < 5; i++ {
					var oe string
					if oe, err = v.RunCommand(ctx, &epb.Command{
						Args: []string{"gsutil", "cp", gcsDest, input.Destination},
					}); err == nil {
						ok = true
						break
					}
					log.Errorf("Failed to download inputs in remote machine %v: %v <Output: %v> --- Retrying...", rc.Name, err, oe)
				}
				if !ok {
					return fmt.Errorf("Error downloading input on remote machine: %v", err)
				}

				if oe, err := v.RunCommand(ctx, &epb.Command{
					Args: []string{"chmod", "+755", input.Destination},
				}); err != nil {
					return fmt.Errorf("Error setting permissions on input: %v <Output: %v>", err, oe)
				}
			}
		}

		return nil
	})
}

func (e *Experiment) runSetup(ctx context.Context) error {
	log.Infof("Will run setup scripts")
	for _, rc := range e.runConfigs {
		s, ok := e.setupScripts[rc.GetName()]
		if !ok {
			var err error
			s, err = e.setupScript(rc)
			if err != nil {
				return err
			}
			e.setupScripts[rc.GetName()] = s
		}
	}
	defer log.Infof("Finished setup scripts")
	return e.runScripts(ctx, e.setupScripts)
}

func (e *Experiment) runBuilds(ctx context.Context) error {
	log.Infof("Will run build scripts")
	for _, rc := range e.runConfigs {
		s, ok := e.buildScripts[rc.GetName()]
		if !ok {
			var err error
			s, err = e.buildScript(rc)
			if err != nil {
				return err
			}
			e.buildScripts[rc.GetName()] = s
		}
	}
	defer log.Infof("Finished build scripts")
	return e.runScripts(ctx, e.buildScripts)
}

func (e *Experiment) runScripts(ctx context.Context, scripts map[string]string) error {
	return e.runOnConfigs(func(rc *epb.RunConfiguration) error {
		script := scripts[rc.GetName()]
		base := filepath.Base(script)
		v, ok := e.vms[rc.GetName()]
		if !ok {
			return fmt.Errorf("could not find VM for run configuration %v", rc.GetName())
		}
		if err := v.CopyFilesToVM(ctx, script, base); err != nil {
			return err
		}
		if _, err := v.RunCommand(ctx, &epb.Command{
			Args: []string{
				"chmod +x " + base,
			}}); err != nil {
			return err
		}
		cCtx, cancel := context.WithCancel(ctx)
		defer cancel()

		if e.logFrequency < 0 {
			if v.IsVM() {
				e.logFrequency = 5
			} else {
				e.logFrequency = 0
			}
		}
		if e.logFrequency > 0 {
			go func() {
				readOE := ""
				ticker := time.NewTicker(time.Duration(e.logFrequency) * time.Second)
				for {
					select {
					case <-ticker.C:
						if oe, err := v.RunCommand(cCtx, &epb.Command{Args: []string{"cat", logFile}}); err == nil {
							delta := strings.TrimPrefix(oe, readOE)
							for _, l := range strings.Split(delta, "\n") {
								if l == "" {
									continue
								}
								log.V(2).Infof("%v: %v", rc.GetName(), l)
							}
							readOE = oe
						}
					case <-cCtx.Done():
						break
					}
				}
			}()
		}
		_, err := v.RunCommand(ctx, &epb.Command{
			Args: []string{
				"./" + base,
			},
		})
		return err
	})
}

func (e *Experiment) downloadReclient(ctx context.Context) error {
	return e.runOnConfigs(func(rc *epb.RunConfiguration) error {
		path := rc.GetReclientBinPath()
		if path == "" {
			return nil
		}
		dest := filepath.Join(e.tmpDir[rc.GetName()], reclientDir)
		if err := e.copyReclientBinaries(ctx, path, dest); err != nil {
			return err
		}
		v, ok := e.vms[rc.GetName()]
		if !ok {
			return fmt.Errorf("could not find VM for run configuration %v", rc.GetName())
		}
		if _, err := v.RunCommand(ctx, &epb.Command{
			Args: []string{
				fmt.Sprintf(v.Sudo("rm -rf %v")+" && "+v.Sudo("mkdir %v")+" && "+v.Sudo("chmod -R 777 %v"),
					rc.ReclientDestination, rc.ReclientDestination, rc.ReclientDestination),
			}}); err != nil {
			return err
		}
		if err := v.CopyFilesToVM(ctx, dest+"/*", rc.ReclientDestination); err != nil {
			return err
		}
		// Set 777 permissions after files are created
		if _, err := v.RunCommand(ctx, &epb.Command{
			Args: []string{
				v.Sudo("chmod -R 777 " + rc.ReclientDestination),
			}}); err != nil {
			return err
		}
		return nil
	})
}

func (e *Experiment) copyReclientBinaries(ctx context.Context, path, dest string) error {
	switch path {
	case "local":
		reclientBinaryPaths := []string{}
		for _, bl := range reclientBinaries {
			binPath, ok := bazel.FindBinary(bl, strings.Split(bl, "/")[1])
			if !ok {
				return fmt.Errorf("Reclient binary %v not found", bl)
			}
			reclientBinaryPaths = append(reclientBinaryPaths, binPath)
		}
		for _, p := range reclientBinaryPaths {
			if err := gcs.Copy(ctx, p, dest); err != nil {
				return err
			}
		}
	default:
		if !strings.HasPrefix(path, "gs://") {
			path = fmt.Sprintf("gs://%v", path)
		}
		if !strings.HasSuffix(path, "/*") {
			path = fmt.Sprintf("%v/*", path)
		}
		if err := gcs.Copy(ctx, path, dest); err != nil {
			return err
		}
	}
	return nil
}

func exportEnv(key, value string) string {
	return fmt.Sprintf("export %v=%v\n", key, value) +
		fmt.Sprintf("echo %v=${%v} >> %v\n", key, key, logFile)
}

func (e *Experiment) exportEnvironmentVariables(rc *epb.RunConfiguration, sb *strings.Builder) {
	sb.WriteString(exportEnv("EXPNAME", e.expPb.GetName()))
	sb.WriteString(exportEnv("RCNAME", rc.GetName()))
	sb.WriteString(exportEnv("RECLIENT_BIN_PATH", rc.GetReclientBinPath()))
	sb.WriteString(exportEnv("RECLIENT_DESTINATION", rc.GetReclientDestination()))
	for _, environment := range rc.GetEnvironment() {
		sb.WriteString(exportEnv(environment.GetKey(), environment.GetValue()))
	}
}

func (e *Experiment) setupScript(rc *epb.RunConfiguration) (string, error) {
	var sb strings.Builder
	sb.WriteString("#!/bin/bash\n")
	sb.WriteString("set -e\n")
	sb.WriteString(fmt.Sprintf("touch %v\n", logFile))
	sb.WriteString(fmt.Sprintf("chmod 666 %v\n", logFile))
	sb.WriteString(fmt.Sprintf("touch %v\n", timeFile))
	sb.WriteString(fmt.Sprintf("chmod 666 %v\n", timeFile))
	sb.WriteString(fmt.Sprintf("echo 0 > %v\n", trialFile))
	sb.WriteString(fmt.Sprintf("chmod 666 %v\n", trialFile))

	e.exportEnvironmentVariables(rc, &sb)

	hasSu := false
	for _, cmd := range rc.GetSetupCommands() {
		for _, a := range cmd.Args {
			if a == "su" {
				hasSu = true
				cmd.Args = append(cmd.Args, "<<'EOSU'")
				break
			}
		}
		sb.WriteString(fmt.Sprintf("echo \"`date`: Will run: %v\" >> %v\n", strings.Join(cmd.Args, " "), logFile))
		sb.WriteString(strings.Join(cmd.Args, " ") + "\n")
		sb.WriteString(fmt.Sprintf("echo \"`date`: Finished: %v\" >> %v\n", strings.Join(cmd.Args, " "), logFile))
	}
	if hasSu {
		sb.WriteString("EOSU\n")
	}
	tmpfile, err := os.CreateTemp(e.tmpDir[rc.GetName()], "setup-*.sh")
	if err != nil {
		return "", err
	}
	if _, err := tmpfile.Write([]byte(sb.String())); err != nil {
		return "", err
	}
	if err := tmpfile.Close(); err != nil {
		return "", err
	}
	log.Infof("Wrote script for %v to %v", rc.GetName(), tmpfile.Name())
	return tmpfile.Name(), nil
}

func (e *Experiment) buildScript(rc *epb.RunConfiguration) (string, error) {
	var sb strings.Builder
	sb.WriteString("#!/bin/bash\n")
	sb.WriteString("set -e\n")
	sb.WriteString(fmt.Sprintf("touch %v\n", logFile))
	sb.WriteString(fmt.Sprintf("chmod 666 %v\n", logFile))
	sb.WriteString(fmt.Sprintf("touch %v\n", timeFile))
	sb.WriteString(fmt.Sprintf("chmod 666 %v\n", timeFile))

	e.exportEnvironmentVariables(rc, &sb)

	sb.WriteString(fmt.Sprintf("trial=$(cat %v)\n", trialFile))
	sb.WriteString(exportEnv("TRIAL", "${trial}"))
	sb.WriteString(fmt.Sprint("trial=$((trial + 1))\n"))
	sb.WriteString(fmt.Sprintf("echo ${trial} > %v\n", trialFile))

	hasSu := false
	for _, cmd := range rc.GetPreBuildCommands() {
		toAppend := ""
		for _, a := range cmd.Args {
			if a == "su" {
				hasSu = true
				toAppend = "<<'EOSU'"
				break
			}
		}
		if toAppend == "" {
			toAppend = " >> " + logFile + " 2>&1 "
		}
		sb.WriteString(fmt.Sprintf("echo \"`date`: Will run: %v\" >> %v\n", strings.Join(cmd.Args, " "), logFile))
		sb.WriteString(strings.Join(cmd.Args, " ") + toAppend + "\n")
		sb.WriteString(fmt.Sprintf("echo \"`date`: Finished: %v\" >> %v\n", strings.Join(cmd.Args, " "), logFile))
	}
	sb.WriteString("START=`date +%s`\n")
	sb.WriteString(fmt.Sprintf("echo \"`date`: Will run: %v\" >> %v\n", strings.Join(rc.BuildCommand.Args, " "), logFile))
	sb.WriteString(strings.Join(rc.BuildCommand.Args, " ") + " >> " + logFile + " 2>&1 " + "\n")
	sb.WriteString(fmt.Sprintf("echo \"`date`: Finished: %v\" >> %v\n", strings.Join(rc.BuildCommand.Args, " "), logFile))
	sb.WriteString("END=`date +%s`\n")
	sb.WriteString(fmt.Sprintf("echo \"$((($END-$START)))s\" > %v\n", timeFile))

	for _, cmd := range rc.GetPostBuildCommands() {
		toAppend := ""
		for _, a := range cmd.Args {
			if a == "su" {
				hasSu = true
				toAppend = "<<'EOSU'"
				break
			}
		}
		if toAppend == "" {
			toAppend = " >> " + logFile + " 2>&1 "
		}
		sb.WriteString(fmt.Sprintf("echo \"`date`: Will run: %v\" >> %v\n", strings.Join(cmd.Args, " "), logFile))
		sb.WriteString(strings.Join(cmd.Args, " ") + toAppend + "\n")
		sb.WriteString(fmt.Sprintf("echo \"`date`: Finished: %v\" >> %v\n", strings.Join(cmd.Args, " "), logFile))
	}

	if hasSu {
		sb.WriteString("EOSU\n")
	}
	tmpfile, err := os.CreateTemp(e.tmpDir[rc.GetName()], "build-*.sh")
	if err != nil {
		return "", err
	}
	if _, err := tmpfile.Write([]byte(sb.String())); err != nil {
		return "", err
	}
	if err := tmpfile.Close(); err != nil {
		return "", err
	}
	log.Infof("Wrote script for %v to %v", rc.GetName(), tmpfile.Name())
	return tmpfile.Name(), nil
}

func (e *Experiment) downloadOutputs(ctx context.Context, trial int) error {
	return e.runOnConfigs(func(rc *epb.RunConfiguration) error {
		v, ok := e.vms[rc.GetName()]
		if !ok {
			return fmt.Errorf("could not find VM for run configuration %v", rc.GetName())
		}
		for _, o := range rc.GetOutputs() {
			if err := v.CopyFilesFromVM(ctx, o, filepath.Join(e.tmpDir[rc.GetName()], outDir)+"/"); err != nil {
				log.Warningf("Couldn't copy file %v: %v", o, err)
			}
		}
		if err := v.CopyFilesFromVM(ctx, timeFile, filepath.Join(e.tmpDir[rc.GetName()], outDir)+"/"); err != nil {
			return err
		}
		if err := v.CopyFilesFromVM(ctx, logFile, filepath.Join(e.tmpDir[rc.GetName()], outDir)+"/"); err != nil {
			return err
		}
		dest := fmt.Sprintf("gs://%v/%v_%v/%v/%v", e.resBucket, e.expPb.GetName(), e.date, rc.GetName(), trial)
		if err := gcs.Copy(ctx, filepath.Join(e.tmpDir[rc.GetName()], outDir)+"/*", dest); err != nil {
			return err
		}
		return nil
	})
}

func (e *Experiment) tearDown(ctx context.Context) error {
	log.Infof("Will run teardown")
	defer log.Infof("Finished teardown")
	return e.runOnConfigs(func(rc *epb.RunConfiguration) error {
		v, ok := e.vms[rc.GetName()]
		if !ok {
			return fmt.Errorf("could not find VM for run configuration %v", rc.GetName())
		}
		if _, err := v.RunCommand(ctx, &epb.Command{
			Args: []string{
				v.Sudo("rm"), logFile, "&&", v.Sudo("rm"), timeFile,
			}}); err != nil {
			return err
		}
		for _, cmd := range rc.GetTeardownCommands() {
			if _, err := v.RunCommand(ctx, cmd); err != nil {
				return err
			}
		}
		return nil
	})
}

func (e *Experiment) rcID(rc *epb.RunConfiguration) string {
	return fmt.Sprintf("%v-%v-%v", e.expPb.GetName(), e.date, rc.GetName())
}

func applyConfig(base, run *epb.RunConfiguration) *epb.RunConfiguration {
	c := proto.Clone(base).(*epb.RunConfiguration)
	c.Name = run.GetName()

	vm.MergeSettings(c.GetMachineSettings(), run.GetMachineSettings())

	c.ReclientBinPath = mergeVal(c.ReclientBinPath, run.GetReclientBinPath())
	c.ReclientDestination = mergeVal(c.ReclientDestination, run.GetReclientDestination())
	c.NumMachines = mergeValUint32(c.NumMachines, run.GetNumMachines())
	c.Inputs = append(c.Inputs, run.GetInputs()...)
	c.SetupCommands = append(c.SetupCommands, run.GetSetupCommands()...)
	c.PreBuildCommands = append(c.PreBuildCommands, run.GetPreBuildCommands()...)
	c.PostBuildCommands = append(c.PostBuildCommands, run.GetPostBuildCommands()...)
	c.Environment = mergeEnvironments(c.GetEnvironment(), run.GetEnvironment())
	if run.GetBuildCommand() != nil {
		c.BuildCommand = run.GetBuildCommand()
	}
	c.Outputs = append(c.Outputs, run.GetOutputs()...)
	c.TeardownCommands = append(c.TeardownCommands, run.GetTeardownCommands()...)
	return c
}

func mergeVal(base, v string) string {
	if v == "" {
		return base
	}
	return v
}

func mergeValEnum(base, v int32) int32 {
	if v == 0 {
		return base
	}
	return v
}

func mergeValUint32(base, v uint32) uint32 {
	if v == 0 {
		return base
	}
	return v
}

func mergeEnvironments(base, run []*epb.Environment) []*epb.Environment {
	if run == nil {
		return base
	} else if base == nil {
		return run
	}
	var e []*epb.Environment
	for _, baseEnv := range base {
		e = append(e, proto.Clone(baseEnv).(*epb.Environment))
	}
	for _, runEnv := range run {
		found := false
		for _, baseEnv := range e {
			if baseEnv.GetKey() == runEnv.GetKey() {
				found = true
				baseEnv.Value = runEnv.GetValue()
			}

		}
		if !found {
			e = append(e, runEnv)
		}
	}
	return e
}
