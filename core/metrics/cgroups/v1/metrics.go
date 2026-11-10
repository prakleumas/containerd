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
	cgroups "github.com/containerd/cgroups/v3/cgroup1"
	"github.com/containerd/containerd/v2/core/metrics/cgroups/internal/collector"
	v1 "github.com/containerd/containerd/v2/core/metrics/types/v1"
	"github.com/docker/go-metrics"
)

// Trigger will be called when an event happens and provides the cgroup
// where the event originated from
type Trigger func(string, string, cgroups.Cgroup)

// NewCollector registers the collector with the provided namespace and returns it so
// that cgroups can be added for collection
func NewCollector(ns *metrics.Namespace) *Collector {
	return collector.New[v1.Metrics](ns, pidMetrics, cpuMetrics, memoryMetrics, hugetlbMetrics, blkioMetrics)
}

// Collector provides the ability to collect container stats and export
// them in the prometheus format
type Collector = collector.Collector[v1.Metrics, *metric]
