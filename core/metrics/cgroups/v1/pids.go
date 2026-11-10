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
	v1 "github.com/containerd/containerd/v2/core/metrics/types/v1"
	metrics "github.com/docker/go-metrics"
	"github.com/prometheus/client_golang/prometheus"
)

var pidMetrics = []*metric{
	pidsGauge("pids", "The limit to the number of pids allowed", metrics.Unit("limit"), (*v1.PidsStat).GetLimit),
	pidsGauge("pids", "The current number of pids", metrics.Unit("current"), (*v1.PidsStat).GetCurrent),
}

// pidsGauge returns a gauge that reports the value get reads from the pids
// stats, and no value when the pids stats are absent.
func pidsGauge(name, help string, unit metrics.Unit, get func(*v1.PidsStat) uint64) *metric {
	return &metric{
		name: name,
		help: help,
		unit: unit,
		vt:   prometheus.GaugeValue,
		getValues: func(stats *v1.Metrics) []value {
			if stats.Pids == nil {
				return nil
			}
			return []value{{v: float64(get(stats.Pids))}}
		},
	}
}
