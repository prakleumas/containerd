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

var cpuMetrics = []*metric{
	cpuGauge("cpu_usage_usec", "Total cpu usage (cgroup v2)", metrics.Unit("microseconds"), (*v2.CPUStat).GetUsageUsec),
	cpuGauge("cpu_user_usec", "Current cpu usage in user space (cgroup v2)", metrics.Unit("microseconds"), (*v2.CPUStat).GetUserUsec),
	cpuGauge("cpu_system_usec", "Current cpu usage in kernel space (cgroup v2)", metrics.Unit("microseconds"), (*v2.CPUStat).GetSystemUsec),
	cpuGauge("cpu_nr_periods", "Current cpu number of periods (only if controller is enabled)", metrics.Total, (*v2.CPUStat).GetNrPeriods),
	cpuGauge("cpu_nr_throttled", "Total number of times tasks have been throttled (only if controller is enabled)", metrics.Total, (*v2.CPUStat).GetNrThrottled),
	cpuGauge("cpu_throttled_usec", "Total time duration for which tasks have been throttled. (only if controller is enabled)", metrics.Unit("microseconds"), (*v2.CPUStat).GetThrottledUsec),
}

// cpuGauge returns a gauge that reports the value get reads from the CPU
// stats, and no value when the CPU stats are absent.
func cpuGauge(name, help string, unit metrics.Unit, get func(*v2.CPUStat) uint64) *metric {
	return &metric{
		name: name,
		help: help,
		unit: unit,
		vt:   prometheus.GaugeValue,
		getValues: func(stats *v2.Metrics) []value {
			if stats.CPU == nil {
				return nil
			}
			return []value{{v: float64(get(stats.CPU))}}
		},
	}
}
