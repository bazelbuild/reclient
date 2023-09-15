package usage

import (
	"os"
	"time"

	log "github.com/golang/glog"
	"github.com/shirou/gopsutil/v3/process"
)

// These are the names of the proxy's resource usage stats.
const (
	CPUPct = "CPU_pct"
	MemPct = "MEM_pct"
	// MemVirt is the total amount of virtual memory used by the current process,
	// includes physically memory used by code, data and libraries, also pages
	// have been swapped out and pages have been mapped but not used yet.
	MemVirt = "MEM_VIRT_mbs"
	// MemRes is a subset of MemVirt, includes the non-swapping physical memory
	// the process is actively using only.
	MemRes        = "MEM_RES_mbs"
	mb     uint64 = 1024 * 1024
)

// PsutilSampler is an instance that reports the usage of OS resource.
type PsutilSampler struct {
	Sampler
}

// Sampler is an interface that can provide the sampled system resource usage.
type Sampler interface {
	Percent(interval time.Duration) (float64, error)
	MemoryPercent() (float32, error)
	MemoryInfo() (*process.MemoryInfoStat, error)
}

// New a PsutilSampler instance with current process's pid.
func New() *PsutilSampler {
	p, err := process.NewProcess(int32(os.Getpid()))
	if err != nil {
		log.Errorf("Cannot create a NewProcess instance to monitor proxy resource usage.")
		return nil
	}
	return &PsutilSampler{p}
}

// Sample returns the current resource usage data by performing a new measurement.
func Sample(uSampler Sampler) map[string]int64 {
	if uSampler == nil {
		return nil
	}
	// resource usage by current proxy's process
	cPercent, err := uSampler.Percent(0)
	if err != nil {
		log.Errorf("Failed to get current process's CPU percent: %v", err)
		return nil
	}
	mPercent, err := uSampler.MemoryPercent()
	if err != nil {
		log.Errorf("Failed to get current process's Mem percent: %v", err)
		return nil
	}
	mInfo, err := uSampler.MemoryInfo()
	if err != nil {
		log.Errorf("Failed to get current process's Mem info: %v", err)
		return nil
	}
	res := map[string]int64{}
	res[CPUPct] = int64(cPercent)
	res[MemPct] = int64(mPercent)
	res[MemVirt] = int64(mInfo.VMS / mb)
	res[MemRes] = int64(mInfo.RSS / mb)
	return res
}
