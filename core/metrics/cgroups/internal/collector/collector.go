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

// Package collector holds the Prometheus collector shared by the cgroup v1
// and v2 metrics packages.
package collector

import (
	"context"
	"fmt"
	"slices"
	"sync"

	cmetrics "github.com/containerd/containerd/v2/core/metrics"
	"github.com/containerd/containerd/v2/core/metrics/cgroups/common"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/containerd/v2/pkg/timeout"
	"github.com/containerd/log"
	"github.com/containerd/typeurl/v2"
	"github.com/docker/go-metrics"
	"github.com/prometheus/client_golang/prometheus"
)

// Metric describes one metric and exports its values from stats of type *S.
type Metric[S any] interface {
	Desc(ns *metrics.Namespace) *prometheus.Desc
	Collect(id, namespace string, stats *S, ns *metrics.Namespace, ch chan<- prometheus.Metric)
}

// New registers the collector with the provided namespace and returns it so
// that cgroups can be added for collection. The collector exports the
// metrics of each table, in order. Stats are unmarshaled into a new S.
func New[S any, M Metric[S]](ns *metrics.Namespace, tables ...[]M) *Collector[S, M] {
	if ns == nil {
		return &Collector[S, M]{}
	}
	c := &Collector[S, M]{
		ns:      ns,
		tasks:   make(map[string]entry),
		metrics: slices.Concat(tables...),
	}
	ns.Add(c)
	return c
}

func taskID(id, namespace string) string {
	return fmt.Sprintf("%s-%s", id, namespace)
}

type entry struct {
	task common.Statable
	// ns is an optional child namespace that contains additional to parent labels.
	// This can be used to append task specific labels to be able to differentiate the different containerd metrics.
	ns *metrics.Namespace
}

// Collector provides the ability to collect container stats and export
// them in the prometheus format
type Collector[S any, M Metric[S]] struct {
	ns *metrics.Namespace

	// TODO(fuweid):
	//
	// The Collector.Collect will be the field ns'Collect's callback,
	// which be invoked periodically with internal lock. And Collector.Add
	// might also invoke ns.Lock if the labels is not nil, which is easy to
	// cause dead-lock.
	//
	// Goroutine X:
	//
	//	ns.Collect
	//   	  ns.Lock
	//          Collector.Collect
	//            Collector.RLock
	//
	//
	// Goroutine Y:
	//
	//	Collector.Add
	//        ...(RLock/Lock)
	//	    ns.Lock
	//
	// I think we should seek the way to decouple ns from Collector.
	mu      sync.RWMutex
	tasks   map[string]entry
	metrics []M
}

// Describe prometheus metrics
func (c *Collector[S, M]) Describe(ch chan<- *prometheus.Desc) {
	for _, m := range c.metrics {
		ch <- m.Desc(c.ns)
	}
}

// Collect prometheus metrics
func (c *Collector[S, M]) Collect(ch chan<- prometheus.Metric) {
	c.mu.RLock()
	wg := &sync.WaitGroup{}
	for _, t := range c.tasks {
		wg.Go(func() { c.collect(t, ch) })
	}
	c.mu.RUnlock()
	wg.Wait()
}

func (c *Collector[S, M]) collect(entry entry, ch chan<- prometheus.Metric) {
	t := entry.task
	ctx, cancel := timeout.WithContext(context.Background(), cmetrics.ShimStatsRequestTimeout)
	stats, err := t.Stats(namespaces.WithNamespace(ctx, t.Namespace()))
	cancel()

	if err != nil {
		log.L.WithError(err).Errorf("stat task %s", t.ID())
		return
	}

	s := new(S)
	if err := typeurl.UnmarshalTo(stats, s); err != nil {
		log.L.WithError(err).Errorf("unmarshal stats for %s", t.ID())
		return
	}

	ns := entry.ns
	if ns == nil {
		ns = c.ns
	}
	for _, m := range c.metrics {
		m.Collect(t.ID(), t.Namespace(), s, ns, ch)
	}
}

// Add adds the provided cgroup and id so that metrics are collected and exported
func (c *Collector[S, M]) Add(t common.Statable, labels map[string]string) error {
	if c.ns == nil {
		return nil
	}
	c.mu.RLock()
	id := taskID(t.ID(), t.Namespace())
	_, ok := c.tasks[id]
	c.mu.RUnlock()
	if ok {
		return nil // requests to collect metrics should be idempotent
	}

	entry := entry{task: t}
	if labels != nil {
		entry.ns = c.ns.WithConstLabels(labels)
	}

	c.mu.Lock()
	c.tasks[id] = entry
	c.mu.Unlock()
	return nil
}

// Remove removes the provided cgroup by id from the collector
func (c *Collector[S, M]) Remove(t common.Statable) {
	if c.ns == nil {
		return
	}
	c.mu.Lock()
	delete(c.tasks, taskID(t.ID(), t.Namespace()))
	c.mu.Unlock()
}

// RemoveAll statable items from the collector
func (c *Collector[S, M]) RemoveAll() {
	if c.ns == nil {
		return
	}
	c.mu.Lock()
	c.tasks = make(map[string]entry)
	c.mu.Unlock()
}
