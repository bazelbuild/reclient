package usage

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/shirou/gopsutil/v3/process"
)

type mockSampler struct {
	idx int
	cpu []float64
	ram []float32
	vms []uint64
	rss []uint64
}

func (m *mockSampler) Percent(interval time.Duration) (float64, error) {
	return m.cpu[m.idx], nil
}

func (m *mockSampler) MemoryPercent() (float32, error) {
	return m.ram[m.idx], nil
}

func (m *mockSampler) MemoryInfo() (*process.MemoryInfoStat, error) {
	p := &process.MemoryInfoStat{VMS: m.vms[m.idx], RSS: m.rss[m.idx]}
	return p, nil
}

func (m *mockSampler) updateIdx() {
	m.idx++
}

var (
	cpuUsage        = []float64{11, 12, 13, 14, 15}
	ramUsage        = []float32{5, 4, 3, 2, 1}
	vmUsage         = []uint64{10 * mb, 20 * mb, 30 * mb, 40 * mb, 50 * mb}
	rssUsage        = []uint64{5 * mb, 4 * mb, 3 * mb, 2 * mb, 1 * mb}
	emptySampler    = &mockSampler{}
	nonEmptySampler = &mockSampler{idx: 0, cpu: cpuUsage, ram: ramUsage, vms: vmUsage, rss: rssUsage}
	wantCPU         = []int64{11, 12, 13, 14, 15}
	wantRAM         = []int64{5, 4, 3, 2, 1}
	wantVIRT        = []int64{10, 20, 30, 40, 50}
	wantRES         = []int64{5, 4, 3, 2, 1}
	wantResults     = map[string][]int64{CPUPct: wantCPU, MemPct: wantRAM, MemVirt: wantVIRT, MemRes: wantRES}
)

func TestNew(t *testing.T) {
	t.Run("New PsutilSampler Instance represents the current process's resource usage", func(t *testing.T) {
		usage := New()
		if usage == nil {
			t.Fatalf("Failed to create a PsutilSampler instance.")
		}
		newUsage := New() // call New() again should return the same process.
		if diff := cmp.Diff(usage.Sampler, newUsage.Sampler, cmpopts.IgnoreUnexported(process.Process{})); diff != "" {
			t.Fatalf("New() returns diff in result: (-want +got)\n%s", diff)
		}
	})
}

func TestSample(t *testing.T) {
	t.Run("AddSample with resource usage data.", func(t *testing.T) {
		for nonEmptySampler.idx <= 4 {
			for key, val := range Sample(nonEmptySampler) {
				want := wantResults[key][nonEmptySampler.idx]
				if diff := cmp.Diff(want, val); diff != "" {
					t.Errorf("Sample() generate diff : (-want +got)\n%s", diff)
				}
			}
			nonEmptySampler.updateIdx()
		}
	})
}
