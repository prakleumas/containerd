//go:build linux

/*
   Copyright The containerd Authors.

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

package v2

import (
	v2 "github.com/containerd/containerd/v2/core/metrics/types/v2"
	metrics "github.com/docker/go-metrics"
	"github.com/prometheus/client_golang/prometheus"
)

var memoryMetrics = []*metric{
	memGauge("memory_usage", "Current memory usage (cgroup v2)", metrics.Bytes, (*v2.MemoryStat).GetUsage),
	memGauge("memory_usage_limit", "Current memory usage limit (cgroup v2)", metrics.Bytes, (*v2.MemoryStat).GetUsageLimit),
	memGauge("memory_swap_usage", "Current swap usage (cgroup v2)", metrics.Bytes, (*v2.MemoryStat).GetSwapUsage),
	memGauge("memory_swap_limit", "Current swap usage limit (cgroup v2)", metrics.Bytes, (*v2.MemoryStat).GetSwapLimit),

	memGauge("memory_file_mapped", "The file_mapped amount", metrics.Bytes, (*v2.MemoryStat).GetFileMapped),
	memGauge("memory_file_dirty", "The file_dirty amount", metrics.Bytes, (*v2.MemoryStat).GetFileDirty),
	memGauge("memory_file_writeback", "The file_writeback amount", metrics.Bytes, (*v2.MemoryStat).GetFileWriteback),
	memGauge("memory_pgactivate", "The pgactivate amount", metrics.Bytes, (*v2.MemoryStat).GetPgactivate),
	memGauge("memory_pgdeactivate", "The pgdeactivate amount", metrics.Bytes, (*v2.MemoryStat).GetPgdeactivate),
	memGauge("memory_pgfault", "The pgfault amount", metrics.Bytes, (*v2.MemoryStat).GetPgfault),
	memGauge("memory_pgmajfault", "The pgmajfault amount", metrics.Bytes, (*v2.MemoryStat).GetPgmajfault),
	memGauge("memory_pglazyfree", "The pglazyfree amount", metrics.Bytes, (*v2.MemoryStat).GetPglazyfree),
	memGauge("memory_pgrefill", "The pgrefill amount", metrics.Bytes, (*v2.MemoryStat).GetPgrefill),
	memGauge("memory_pglazyfreed", "The pglazyfreed amount", metrics.Bytes, (*v2.MemoryStat).GetPglazyfreed),
	memGauge("memory_pgscan", "The pgscan amount", metrics.Bytes, (*v2.MemoryStat).GetPgscan),
	memGauge("memory_pgsteal", "The pgsteal amount", metrics.Bytes, (*v2.MemoryStat).GetPgsteal),
	memGauge("memory_inactive_anon", "The inactive_anon amount", metrics.Bytes, (*v2.MemoryStat).GetInactiveAnon),
	memGauge("memory_active_anon", "The active_anon amount", metrics.Bytes, (*v2.MemoryStat).GetActiveAnon),
	memGauge("memory_inactive_file", "The inactive_file amount", metrics.Bytes, (*v2.MemoryStat).GetInactiveFile),
	memGauge("memory_active_file", "The active_file amount", metrics.Bytes, (*v2.MemoryStat).GetActiveFile),
	memGauge("memory_unevictable", "The unevictable amount", metrics.Bytes, (*v2.MemoryStat).GetUnevictable),
	memGauge("memory_anon", "The anon amount", metrics.Bytes, (*v2.MemoryStat).GetAnon),
	memGauge("memory_file", "The file amount", metrics.Bytes, (*v2.MemoryStat).GetFile),
	memGauge("memory_kernel_stack", "The kernel_stack amount", metrics.Bytes, (*v2.MemoryStat).GetKernelStack),
	memGauge("memory_slab", "The slab amount", metrics.Bytes, (*v2.MemoryStat).GetSlab),
	memGauge("memory_sock", "The sock amount", metrics.Bytes, (*v2.MemoryStat).GetSock),
	memGauge("memory_shmem", "The shmem amount", metrics.Bytes, (*v2.MemoryStat).GetShmem),
	memGauge("memory_anon_thp", "The anon_thp amount", metrics.Bytes, (*v2.MemoryStat).GetAnonThp),
	memGauge("memory_slab_reclaimable", "The slab_reclaimable amount", metrics.Bytes, (*v2.MemoryStat).GetSlabReclaimable),
	memGauge("memory_slab_unreclaimable", "The slab_unreclaimable amount", metrics.Bytes, (*v2.MemoryStat).GetSlabUnreclaimable),
	memGauge("memory_workingset_refault", "The workingset_refault amount", metrics.Bytes, (*v2.MemoryStat).GetWorkingsetRefault),
	memGauge("memory_workingset_activate", "The workingset_activate amount", metrics.Bytes, (*v2.MemoryStat).GetWorkingsetActivate),
	memGauge("memory_workingset_nodereclaim", "The workingset_nodereclaim amount", metrics.Bytes, (*v2.MemoryStat).GetWorkingsetNodereclaim),
	memGauge("memory_thp_fault_alloc", "The thp_fault_alloc amount", metrics.Bytes, (*v2.MemoryStat).GetThpFaultAlloc),
	memGauge("memory_thp_collapse_alloc", "The thp_collapse_alloc amount", metrics.Bytes, (*v2.MemoryStat).GetThpCollapseAlloc),
	{
		name: "memory_oom",
		help: "The number of times a container has received an oom event",
		unit: metrics.Total,
		vt:   prometheus.GaugeValue,
		getValues: func(stats *v2.Metrics) []value {
			if stats.MemoryEvents == nil {
				return nil
			}
			return []value{
				{
					v: float64(stats.MemoryEvents.Oom),
				},
			}
		},
	},
}

// memGauge returns a gauge that reports the value get reads from the memory
// stats, and no value when the memory stats are absent.
func memGauge(name, help string, unit metrics.Unit, get func(*v2.MemoryStat) uint64) *metric {
	return &metric{
		name: name,
		help: help,
		unit: unit,
		vt:   prometheus.GaugeValue,
		getValues: func(stats *v2.Metrics) []value {
			if stats.Memory == nil {
				return nil
			}
			return []value{{v: float64(get(stats.Memory))}}
		},
	}
}
