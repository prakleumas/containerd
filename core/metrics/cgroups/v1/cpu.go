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
	"strconv"

	v1 "github.com/containerd/containerd/v2/core/metrics/types/v1"
	metrics "github.com/docker/go-metrics"
	"github.com/prometheus/client_golang/prometheus"
)

var cpuMetrics = []*metric{
	cpuGauge("cpu_total", "The total cpu time", metrics.Nanoseconds, func(s *v1.CPUStat) uint64 { return s.Usage.Total }),
	cpuGauge("cpu_kernel", "The total kernel cpu time", metrics.Nanoseconds, func(s *v1.CPUStat) uint64 { return s.Usage.Kernel }),
	cpuGauge("cpu_user", "The total user cpu time", metrics.Nanoseconds, func(s *v1.CPUStat) uint64 { return s.Usage.User }),
	{
		name:   "per_cpu",
		help:   "The total cpu time per cpu",
		unit:   metrics.Nanoseconds,
		vt:     prometheus.GaugeValue,
		labels: []string{"cpu"},
		getValues: func(stats *v1.Metrics) []value {
			if stats.CPU == nil {
				return nil
			}
			var out []value
			for i, v := range stats.CPU.Usage.PerCPU {
				out = append(out, value{
					v: float64(v),
					l: []string{strconv.Itoa(i)},
				})
			}
			return out
		},
	},
	cpuGauge("cpu_throttle_periods", "The total cpu throttle periods", metrics.Total, func(s *v1.CPUStat) uint64 { return s.Throttling.Periods }),
	cpuGauge("cpu_throttled_periods", "The total cpu throttled periods", metrics.Total, func(s *v1.CPUStat) uint64 { return s.Throttling.ThrottledPeriods }),
	cpuGauge("cpu_throttled_time", "The total cpu throttled time", metrics.Nanoseconds, func(s *v1.CPUStat) uint64 { return s.Throttling.ThrottledTime }),
}

// cpuGauge returns a gauge that reports the value get reads from the CPU
// stats, and no value when the CPU stats are absent.
func cpuGauge(name, help string, unit metrics.Unit, get func(*v1.CPUStat) uint64) *metric {
	return &metric{
		name: name,
		help: help,
		unit: unit,
		vt:   prometheus.GaugeValue,
		getValues: func(stats *v1.Metrics) []value {
			if stats.CPU == nil {
				return nil
			}
			return []value{{v: float64(get(stats.CPU))}}
		},
	}
}
