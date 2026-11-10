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

package v1

import (
	cgroup1stats "github.com/containerd/cgroups/v3/cgroup1/stats"
	v1 "github.com/containerd/containerd/v2/core/metrics/types/v1"
	metrics "github.com/docker/go-metrics"
	"github.com/prometheus/client_golang/prometheus"
)

var memoryMetrics = []*metric{
	memGauge("memory_cache", "The cache amount used", metrics.Bytes, (*v1.MemoryStat).GetCache),
	memGauge("memory_rss", "The rss amount used", metrics.Bytes, (*v1.MemoryStat).GetRSS),
	memGauge("memory_rss_huge", "The rss_huge amount used", metrics.Bytes, (*v1.MemoryStat).GetRSSHuge),
	memGauge("memory_mapped_file", "The mapped_file amount used", metrics.Bytes, (*v1.MemoryStat).GetMappedFile),
	memGauge("memory_dirty", "The dirty amount", metrics.Bytes, (*v1.MemoryStat).GetDirty),
	memGauge("memory_writeback", "The writeback amount", metrics.Bytes, (*v1.MemoryStat).GetWriteback),
	memGauge("memory_pgpgin", "The pgpgin amount", metrics.Bytes, (*v1.MemoryStat).GetPgPgIn),
	memGauge("memory_pgpgout", "The pgpgout amount", metrics.Bytes, (*v1.MemoryStat).GetPgPgOut),
	memGauge("memory_pgfault", "The pgfault amount", metrics.Bytes, (*v1.MemoryStat).GetPgFault),
	memGauge("memory_pgmajfault", "The pgmajfault amount", metrics.Bytes, (*v1.MemoryStat).GetPgMajFault),
	memGauge("memory_inactive_anon", "The inactive_anon amount", metrics.Bytes, (*v1.MemoryStat).GetInactiveAnon),
	memGauge("memory_active_anon", "The active_anon amount", metrics.Bytes, (*v1.MemoryStat).GetActiveAnon),
	memGauge("memory_inactive_file", "The inactive_file amount", metrics.Bytes, (*v1.MemoryStat).GetInactiveFile),
	memGauge("memory_active_file", "The active_file amount", metrics.Bytes, (*v1.MemoryStat).GetActiveFile),
	memGauge("memory_unevictable", "The unevictable amount", metrics.Bytes, (*v1.MemoryStat).GetUnevictable),
	memGauge("memory_hierarchical_memory_limit", "The hierarchical_memory_limit amount", metrics.Bytes, (*v1.MemoryStat).GetHierarchicalMemoryLimit),
	memGauge("memory_hierarchical_memsw_limit", "The hierarchical_memsw_limit amount", metrics.Bytes, (*v1.MemoryStat).GetHierarchicalSwapLimit),
	memGauge("memory_total_cache", "The total_cache amount used", metrics.Bytes, (*v1.MemoryStat).GetTotalCache),
	memGauge("memory_total_rss", "The total_rss amount used", metrics.Bytes, (*v1.MemoryStat).GetTotalRSS),
	memGauge("memory_total_rss_huge", "The total_rss_huge amount used", metrics.Bytes, (*v1.MemoryStat).GetTotalRSSHuge),
	memGauge("memory_total_mapped_file", "The total_mapped_file amount used", metrics.Bytes, (*v1.MemoryStat).GetTotalMappedFile),
	memGauge("memory_total_dirty", "The total_dirty amount", metrics.Bytes, (*v1.MemoryStat).GetTotalDirty),
	memGauge("memory_total_writeback", "The total_writeback amount", metrics.Bytes, (*v1.MemoryStat).GetTotalWriteback),
	memGauge("memory_total_pgpgin", "The total_pgpgin amount", metrics.Bytes, (*v1.MemoryStat).GetTotalPgPgIn),
	memGauge("memory_total_pgpgout", "The total_pgpgout amount", metrics.Bytes, (*v1.MemoryStat).GetTotalPgPgOut),
	memGauge("memory_total_pgfault", "The total_pgfault amount", metrics.Bytes, (*v1.MemoryStat).GetTotalPgFault),
	memGauge("memory_total_pgmajfault", "The total_pgmajfault amount", metrics.Bytes, (*v1.MemoryStat).GetTotalPgMajFault),
	memGauge("memory_total_inactive_anon", "The total_inactive_anon amount", metrics.Bytes, (*v1.MemoryStat).GetTotalInactiveAnon),
	memGauge("memory_total_active_anon", "The total_active_anon amount", metrics.Bytes, (*v1.MemoryStat).GetTotalActiveAnon),
	memGauge("memory_total_inactive_file", "The total_inactive_file amount", metrics.Bytes, (*v1.MemoryStat).GetTotalInactiveFile),
	memGauge("memory_total_active_file", "The total_active_file amount", metrics.Bytes, (*v1.MemoryStat).GetTotalActiveFile),
	memGauge("memory_total_unevictable", "The total_unevictable amount", metrics.Bytes, (*v1.MemoryStat).GetTotalUnevictable),
	memEntryGauge("memory_usage_failcnt", "The usage failcnt", metrics.Total, (*v1.MemoryStat).GetUsage, (*cgroup1stats.MemoryEntry).GetFailcnt),
	memEntryGauge("memory_usage_limit", "The memory limit", metrics.Bytes, (*v1.MemoryStat).GetUsage, (*cgroup1stats.MemoryEntry).GetLimit),
	memEntryGauge("memory_usage_max", "The memory maximum usage", metrics.Bytes, (*v1.MemoryStat).GetUsage, (*cgroup1stats.MemoryEntry).GetMax),
	memEntryGauge("memory_usage_usage", "The memory usage", metrics.Bytes, (*v1.MemoryStat).GetUsage, (*cgroup1stats.MemoryEntry).GetUsage),
	memEntryGauge("memory_swap_failcnt", "The swap failcnt", metrics.Total, (*v1.MemoryStat).GetSwap, (*cgroup1stats.MemoryEntry).GetFailcnt),
	memEntryGauge("memory_swap_limit", "The swap limit", metrics.Bytes, (*v1.MemoryStat).GetSwap, (*cgroup1stats.MemoryEntry).GetLimit),
	memEntryGauge("memory_swap_max", "The swap maximum usage", metrics.Bytes, (*v1.MemoryStat).GetSwap, (*cgroup1stats.MemoryEntry).GetMax),
	memEntryGauge("memory_swap_usage", "The swap usage", metrics.Bytes, (*v1.MemoryStat).GetSwap, (*cgroup1stats.MemoryEntry).GetUsage),
	memEntryGauge("memory_kernel_failcnt", "The kernel failcnt", metrics.Total, (*v1.MemoryStat).GetKernel, (*cgroup1stats.MemoryEntry).GetFailcnt),
	memEntryGauge("memory_kernel_limit", "The kernel limit", metrics.Bytes, (*v1.MemoryStat).GetKernel, (*cgroup1stats.MemoryEntry).GetLimit),
	memEntryGauge("memory_kernel_max", "The kernel maximum usage", metrics.Bytes, (*v1.MemoryStat).GetKernel, (*cgroup1stats.MemoryEntry).GetMax),
	memEntryGauge("memory_kernel_usage", "The kernel usage", metrics.Bytes, (*v1.MemoryStat).GetKernel, (*cgroup1stats.MemoryEntry).GetUsage),
	memEntryGauge("memory_kerneltcp_failcnt", "The kerneltcp failcnt", metrics.Total, (*v1.MemoryStat).GetKernelTCP, (*cgroup1stats.MemoryEntry).GetFailcnt),
	memEntryGauge("memory_kerneltcp_limit", "The kerneltcp limit", metrics.Bytes, (*v1.MemoryStat).GetKernelTCP, (*cgroup1stats.MemoryEntry).GetLimit),
	memEntryGauge("memory_kerneltcp_max", "The kerneltcp maximum usage", metrics.Bytes, (*v1.MemoryStat).GetKernelTCP, (*cgroup1stats.MemoryEntry).GetMax),
	memEntryGauge("memory_kerneltcp_usage", "The kerneltcp usage", metrics.Bytes, (*v1.MemoryStat).GetKernelTCP, (*cgroup1stats.MemoryEntry).GetUsage),
}

// memGauge returns a gauge that reports the value get reads from the memory
// stats, and no value when the memory stats are absent.
func memGauge(name, help string, unit metrics.Unit, get func(*v1.MemoryStat) uint64) *metric {
	return &metric{
		name: name,
		help: help,
		unit: unit,
		vt:   prometheus.GaugeValue,
		getValues: func(stats *v1.Metrics) []value {
			if stats.Memory == nil {
				return nil
			}
			return []value{{v: float64(get(stats.Memory))}}
		},
	}
}

// memEntryGauge returns a gauge that reports the value get reads from the
// memory entry that entry selects, and no value when that entry is absent.
func memEntryGauge(name, help string, unit metrics.Unit, entry func(*v1.MemoryStat) *cgroup1stats.MemoryEntry, get func(*cgroup1stats.MemoryEntry) uint64) *metric {
	return &metric{
		name: name,
		help: help,
		unit: unit,
		vt:   prometheus.GaugeValue,
		getValues: func(stats *v1.Metrics) []value {
			e := entry(stats.GetMemory())
			if e == nil {
				return nil
			}
			return []value{{v: float64(get(e))}}
		},
	}
}
