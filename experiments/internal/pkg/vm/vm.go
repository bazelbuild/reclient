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

// Package vm contains logic relevant to VMs used during an experiment.
package vm

import (
	"context"
	"fmt"
	"os/exec"
	"reflect"
	"regexp"
	"strings"
	"time"

	epb "team/foundry-x/re-client/experiments/api/experiment"

	log "github.com/golang/glog"
)

// VM is a struct holding information about an experiment VM.
type VM struct {
	name       string
	diskName   string
	project    string
	vmSettings *epb.VMSettings
	wsSettings *epb.WSSettings
}

// NewMachine prepares the machine for connection
func NewMachine(name, project string, settings interface{}) *VM {
	switch m := settings.(type) {
	case *epb.RunConfiguration_VmSettings:
		return &VM{
			name:       sanitizeName(name),
			project:    project,
			vmSettings: m.VmSettings,
			wsSettings: nil,
		}
	case *epb.RunConfiguration_WsSettings:
		return &VM{
			name:       m.WsSettings.GetAddress(),
			project:    project,
			vmSettings: nil,
			wsSettings: m.WsSettings,
		}
	default:
		log.Fatal("RunConfiguration is missing one of vm_settings or ws_settings")
	}
	return nil
}

func sanitizeName(name string) string {
	var re = regexp.MustCompile(`[^a-zA-Z0-9\-]`)
	return strings.ToLower(re.ReplaceAllString(name, "-"))
}

func (v *VM) gcePrefix() []string {
	return []string{"gcloud", "compute", "--project=" + v.project}
}

func (v *VM) sshUser() string {
	var sshUser string
	if v.vmSettings != nil {
		sshUser = v.vmSettings.GetSshUser()
	} else if v.wsSettings != nil {
		sshUser = v.wsSettings.GetSshUser()
	}
	if sshUser != "" {
		sshUser += "@"
	}
	return sshUser
}

func (v *VM) sshKey() []string {
	if v.vmSettings != nil {
		if v.vmSettings.GetSshKeyPath() != "" {
			return []string{"--ssh-key-file=" + v.vmSettings.GetSshKeyPath()}
		}
	} else if v.wsSettings != nil {
		if v.wsSettings.GetSshKeyPath() != "" {
			return []string{"-i" + v.wsSettings.GetSshKeyPath()}
		}
	}
	return []string{}
}

func (v *VM) sshPrefix() []string {
	if v.vmSettings != nil {
		cmd := append(v.gcePrefix(), "ssh", "--ssh-flag=-tt", v.sshUser()+v.name, "--zone="+v.vmSettings.GetZone())
		cmd = append(cmd, v.sshKey()...)
		return append(cmd, "--")
	} else if v.wsSettings != nil {
		cmd := []string{"ssh", v.sshUser() + v.name}
		cmd = append(cmd, v.sshKey()...)
		return cmd
	}
	return []string{}
}

func (v *VM) scpPrefix() []string {
	if v.vmSettings != nil {
		cmd := append(v.gcePrefix(), "scp", "--zone="+v.vmSettings.GetZone())
		cmd = append(cmd, v.sshKey()...)
		return append(cmd, "--")
	} else if v.wsSettings != nil {
		cmd := []string{"scp"}
		cmd = append(cmd, v.sshKey()...)
		return cmd
	}
	return []string{}
}

// Name returns the name of the VM.
func (v *VM) Name() string {
	return v.name
}

// CreateWithDisk creates the VM and attaches an image disk to it.
func (v *VM) CreateWithDisk(ctx context.Context) error {
	if v.vmSettings == nil {
		log.Infof("Not a VM; skipped creating workstation %v", v.name)
		return nil
	}

	log.Infof("Creating VM %v", v.name)
	if v.vmSettings.GetImage() == "" && v.vmSettings.GetImageProject() == "" {
		return fmt.Errorf("no boot image configuration found")
	}
	if v.vmSettings.GetDiskImage() == "" && v.vmSettings.GetDiskImageProject() == "" {
		return fmt.Errorf("no disk image configuration found")
	}

	args := append(v.gcePrefix(),
		"instances", "create", v.name,
		"--zone="+v.vmSettings.GetZone(), "--image="+v.vmSettings.GetImage(),
		"--scopes=https://www.googleapis.com/auth/cloud-platform,https://www.googleapis.com/auth/userinfo.email",
		"--image-project="+v.vmSettings.GetImageProject(),
		"--machine-type="+v.vmSettings.GetMachineType())

	v.diskName = v.name + "-source-disk"
	diskType := "pd-ssd"
	if v.vmSettings.GetDiskType() != "" {
		diskType = v.vmSettings.GetDiskType()
	}
	createDiskArg := fmt.Sprintf("--create-disk=name=%s,image=%s,image-project=%s,type=%s",
		v.diskName, v.vmSettings.GetDiskImage(), v.vmSettings.GetDiskImageProject(), diskType)

	args = append(args, createDiskArg)
	args = append(args, v.vmSettings.GetCreationFlags()...)
	log.Infof("Args: %v", args)
	if oe, err := exec.CommandContext(ctx, args[0], args[1:]...).CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create VM: %v, outerr: %v", err, string(oe))
	}
	log.Infof("Created VM %v", v.name)

	// Choosing an SSH user is incompatible with OS login.
	// Also notice that GCE Windows VM do not support OS login.
	if v.vmSettings.GetSshUser() != "" {
		args = append(v.gcePrefix(), "instances", "add-metadata", v.name,
			"--zone="+v.vmSettings.GetZone(),
			"--metadata", "enable-oslogin=false")
		if oe, err := exec.CommandContext(ctx, args[0], args[1:]...).CombinedOutput(); err != nil {
			return fmt.Errorf("failed to change VM metadata: %v, outerr: %v", err, string(oe))
		}

		log.Infof("Disabled OS login on %v", v.name)
	}

	// resizes system disk to a size specified in VM settings
	if v.vmSettings.GetSystemDiskSize() != "" {
		args = append(v.gcePrefix(), "disks", "resize", v.name,
			"--size="+v.vmSettings.GetSystemDiskSize(),
			"--zone="+v.vmSettings.GetZone(),
			"--quiet")
		if oe, err := exec.CommandContext(ctx, args[0], args[1:]...).CombinedOutput(); err != nil {
			return fmt.Errorf("failed to resize system disk size of VM: %v, outerr: %v", err, string(oe))
		}
		log.Infof("Resized system disk to %v", v.vmSettings.GetSystemDiskSize())
	}
	return nil
}

// Mount mounts the attached disk to the VM.
func (v *VM) Mount(ctx context.Context) error {
	if v.vmSettings == nil {
		log.Infof("Not a VM; skipped mounting disks to workstation %v", v.name)
		return nil
	}

	log.Infof("Mounting disk %v", v.name)
	if v.vmSettings.GetDiskImage() == "" && v.vmSettings.GetDiskImageProject() == "" {
		return fmt.Errorf("no disk configuration found")
	}
	if err := v.waitUntilUp(ctx); err != nil {
		return err
	}

	var err error
	if v.vmSettings.GetImageOs() == epb.VMSettings_WINDOWS {
		err = v.mountWin(ctx)
	} else {
		err = v.mountLinux(ctx)
	}

	if err != nil {
		return err
	}

	log.Infof("Mounted disk %v", v.name)
	return nil
}

func (v *VM) mountLinux(ctx context.Context) error {
	mountPoint := "/img"
	if _, err := v.RunCommand(ctx, &epb.Command{
		Args: []string{
			"sudo", "mkdir", "-p", mountPoint,
		},
	}); err != nil {
		return fmt.Errorf("failed to mkdir mount point: %v", err)
	}
	if _, err := v.RunCommand(ctx, &epb.Command{
		Args: []string{
			"sudo", "mount", "-o", "discard,defaults", "/dev/sdb", mountPoint,
		},
	}); err != nil {
		return fmt.Errorf("failed to mount disk: %v", err)
	}
	return nil
}

// Mount mounts the attached disk to the Windows VM.
func (v *VM) mountWin(ctx context.Context) error {
	// From some testing, Windows seems to already mount the image correctly
	// at D:\ as long as it's been previously formatted.
	return nil
}

// ImageDisk takes an image from the attached disk.
func (v *VM) ImageDisk(ctx context.Context) error {
	if v.vmSettings == nil {
		log.Infof("Not a VM; skipped imaging disks from workstation %v", v.name)
		return nil
	}

	log.Infof("Imaging disk %v", v.name)
	if v.vmSettings.GetDiskImage() == "" && v.vmSettings.GetDiskImageProject() == "" {
		return fmt.Errorf("no disk configuration found")
	}
	name := v.name + "-source-disk"
	args := append(v.gcePrefix(),
		"images", "create", fmt.Sprintf("%v-%v", v.name, v.vmSettings.GetDiskImage()),
		"--zone="+v.vmSettings.GetZone(),
		"--source-disk="+v.diskName,
		"--source-disk-zone="+v.vmSettings.GetZone())
	if oe, err := exec.CommandContext(ctx, args[0], args[1:]...).CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create disk %v: %v, outerr: %v", name, err, string(oe))
	}
	log.Infof("Imaged disk %v", v.name)
	return nil
}

// CopyFilesToVM copies files from the local machine to the VM.
func (v *VM) CopyFilesToVM(ctx context.Context, src, dest string) error {
	log.Infof("Copying files %v", src)
	args := append(v.scpPrefix(),
		src, fmt.Sprintf("%v%v:%v", v.sshUser(), v.name, dest))
	log.Infof("Args: %v", args)
	// Use bash here to expand * when copying multiple files.
	if oe, err := exec.CommandContext(ctx, "/bin/bash", "-c", strings.Join(args, " ")).CombinedOutput(); err != nil {
		return fmt.Errorf("failed to copy files to VM: %v, outerr: %v", err, string(oe))
	}
	log.Infof("Copied files %v", src)
	return nil
}

// CopyFilesFromVM copies files from the VM to the local machine.
func (v *VM) CopyFilesFromVM(ctx context.Context, src, dest string) error {
	log.Infof("Copying files %v", src)
	args := append(v.scpPrefix(),
		fmt.Sprintf("%v%v:%v", v.sshUser(), v.name, src), dest)
	log.Infof("Args: %v", args)
	// Use bash here to expand * when copying multiple files.
	if oe, err := exec.CommandContext(ctx, "/bin/bash", "-c", strings.Join(args, " ")).CombinedOutput(); err != nil {
		return fmt.Errorf("failed to copy files from VM: %v, outerr: %v", err, string(oe))
	}
	log.Infof("Copied files %v", src)
	return nil
}

func (v *VM) waitUntilUp(ctx context.Context) error {
	for _, i := range []int{10, 30, 60, 120} {
		time.Sleep(time.Duration(i) * time.Second)
		if _, err := v.RunCommand(ctx, &epb.Command{
			Args: []string{
				"echo", "Hello",
			},
		}); err == nil {
			return nil
		}
	}
	return fmt.Errorf("VM %v did not startup in time", v.name)
}

// Delete deletes the VM image. If dryRun, it just returns the command that will
// delete the VM.
func (v *VM) Delete(ctx context.Context, dryRun bool) (string, error) {
	if v.vmSettings == nil {
		log.Infof("Not a VM; skipped deleting workstation %v", v.name)
		return "", nil
	}

	log.Infof("Deleting VM %v (DryRun: %v)", v.name, dryRun)
	args := append(v.gcePrefix(), "instances", "delete", v.name,
		"--zone="+v.vmSettings.GetZone())
	if !dryRun {
		if oe, err := exec.CommandContext(ctx, args[0], args[1:]...).CombinedOutput(); err != nil {
			return "", fmt.Errorf("failed to delete VM: %v, outerr: %v", err, string(oe))
		}
	}
	log.Infof("Deleted VM %v (DryRun: %v)", v.name, dryRun)
	return strings.Join(args, " "), nil
}

// RunCommand runs a command on the VM.
func (v *VM) RunCommand(ctx context.Context, cmd *epb.Command) (string, error) {
	log.V(3).Infof("Running command %+v on VM %v", cmd, v.name)
	args := append(v.sshPrefix(), strings.Join(cmd.Args, " "))
	oe, err := exec.CommandContext(ctx, args[0], args[1:]...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to run command on %v: %v, outerr: %v", v.name, err, string(oe))
	}
	log.V(3).Infof("Ran command %+v on VM %v", cmd, v.name)
	return string(oe), nil
}

// IsVM returns true if the machine is a created virtual machine
func (v *VM) IsVM() bool {
	return v.vmSettings != nil
}

// IsWS returns true if the machine is a pre-existing workstation
func (v *VM) IsWS() bool {
	return v.wsSettings != nil
}

// Sudo adds "sudo" to a command if possible for the machine
func (v *VM) Sudo(cmd string) string {
	if v.vmSettings != nil {
		return "sudo " + cmd
	}
	if v.wsSettings != nil && v.wsSettings.UseSudo == true {
		return "sudo " + cmd
	}
	return cmd
}

// MergeSettings merges "run" settings into "base" settings
func MergeSettings(baseSettings, runSettings interface{}) {
	if baseSettings == nil {
		log.Fatalf("Machine settings are required in the base configuration")
	}
	if runSettings == nil {
		// Nothing to merge
		return
	}

	if reflect.TypeOf(baseSettings) != reflect.TypeOf(runSettings) {
		log.Fatalf("Base and run configurations must have the same machine settings type: base is %T but run is %T", baseSettings, runSettings)
	}

	switch machineSettings := baseSettings.(type) {
	case *epb.RunConfiguration_VmSettings:
		run := runSettings.(*epb.RunConfiguration_VmSettings)
		machineSettings.VmSettings.Zone = mergeVal(machineSettings.VmSettings.Zone, run.VmSettings.GetZone())
		machineSettings.VmSettings.MachineType = mergeVal(machineSettings.VmSettings.MachineType, run.VmSettings.GetMachineType())
		machineSettings.VmSettings.Image = mergeVal(machineSettings.VmSettings.Image, run.VmSettings.GetImage())
		machineSettings.VmSettings.ImageOs = epb.VMSettings_OS(mergeValEnum(int32(machineSettings.VmSettings.ImageOs), int32(run.VmSettings.GetImageOs())))
		machineSettings.VmSettings.ImageProject = mergeVal(machineSettings.VmSettings.ImageProject, run.VmSettings.GetImageProject())
		machineSettings.VmSettings.SshUser = mergeVal(machineSettings.VmSettings.SshUser, run.VmSettings.GetSshUser())
		machineSettings.VmSettings.SshKeyPath = mergeVal(machineSettings.VmSettings.SshKeyPath, run.VmSettings.GetSshKeyPath())
		machineSettings.VmSettings.DiskImage = mergeVal(machineSettings.VmSettings.DiskImage, run.VmSettings.GetDiskImage())
		machineSettings.VmSettings.DiskImageProject = mergeVal(machineSettings.VmSettings.DiskImageProject, run.VmSettings.GetDiskImageProject())
		machineSettings.VmSettings.DiskType = mergeVal(machineSettings.VmSettings.DiskType, run.VmSettings.GetDiskType())
		machineSettings.VmSettings.CreationFlags = append(machineSettings.VmSettings.CreationFlags, run.VmSettings.GetCreationFlags()...)
	case *epb.RunConfiguration_WsSettings:
		run := runSettings.(*epb.RunConfiguration_WsSettings)
		machineSettings.WsSettings.Address = mergeVal(machineSettings.WsSettings.Address, run.WsSettings.GetAddress())
		machineSettings.WsSettings.SshUser = mergeVal(machineSettings.WsSettings.SshUser, run.WsSettings.GetSshUser())
		machineSettings.WsSettings.SshKeyPath = mergeVal(machineSettings.WsSettings.SshKeyPath, run.WsSettings.GetSshKeyPath())
	default:
		log.Fatalf("Unknown configuration type: %T (%T)", baseSettings, runSettings)
	}

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
