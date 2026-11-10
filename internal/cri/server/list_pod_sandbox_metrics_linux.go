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

package server

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	cg1 "github.com/containerd/cgroups/v3/cgroup1/stats"
	cg2 "github.com/containerd/cgroups/v3/cgroup2/stats"
	"github.com/containerd/errdefs"
	"github.com/containerd/log"
	"github.com/containerd/typeurl/v2"
	"golang.org/x/sync/errgroup"
	"golang.org/x/time/rate"
	runtime "k8s.io/cri-api/pkg/apis/runtime/v1"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/core/containers"
	containerstore "github.com/containerd/containerd/v2/internal/cri/store/container"
	sandboxstore "github.com/containerd/containerd/v2/internal/cri/store/sandbox"
	"github.com/containerd/containerd/v2/internal/cri/util"
	"github.com/containerd/containerd/v2/plugins"
)

// Rate limiter to prevent overwhelming the system with concurrent requests
var limiter = rate.NewLimiter(rate.Limit(10), 10) // Allow 10 concurrent requests with burst of 10

// ListPodSandboxMetrics gets pod sandbox metrics from CRI Runtime
func (c *criService) ListPodSandboxMetrics(ctx context.Context, r *runtime.ListPodSandboxMetricsRequest) (*runtime.ListPodSandboxMetricsResponse, error) {
	ctx = util.WithNamespace(ctx)

	// Create errgroup with context and limit concurrency to 10
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(10)

	// Fetch all containers before executing any metric collection
	// to correctly map them to each sandbox.
	sandboxContainerMap := make(map[string][]containerstore.Container)
	for _, container := range c.containerStore.List() {
		sandboxContainerMap[container.SandboxID] = append(sandboxContainerMap[container.SandboxID], container)
	}

	var (
		podSandboxMetrics []*runtime.PodSandboxMetrics
		metricsMu         sync.Mutex
	)
	for _, sandbox := range c.sandboxStore.List() {
		// Only collect metrics for ready sandboxes
		if sandbox.Status.Get().State != sandboxstore.StateReady {
			continue
		}

		// Wait for rate limiter permission
		if err := limiter.Wait(ctx); err != nil {
			log.G(ctx).WithError(err).Debug("rate limiter context cancelled")
			return nil, err
		}

		g.Go(func() error {
			// Extract pod-level labels
			baseLabels := []string{sandbox.Config.GetMetadata().GetName(), sandbox.Config.GetMetadata().GetNamespace()}

			sandboxMetrics, err := c.collectPodSandboxMetrics(gctx, sandbox, baseLabels)
			if err != nil {
				switch {
				case errdefs.IsUnavailable(err), errdefs.IsNotFound(err):
					log.G(gctx).WithField("podsandboxid", sandbox.ID).WithError(err).Error("failed to get pod sandbox metrics, this is likely a transient error")
					// Don't return error for transient issues, just log and continue
					return nil
				case errdefs.IsCanceled(err):
					log.G(gctx).WithField("podsandboxid", sandbox.ID).WithError(err).Debug("metrics collection cancelled")
					// Return the cancellation error to stop other goroutines
					return err
				default:
					log.G(gctx).WithField("podsandboxid", sandbox.ID).WithError(err).Error("failed to collect pod sandbox metrics")
					// Don't return error for individual failures, just log and continue
					return nil
				}
			}

			for _, container := range sandboxContainerMap[sandbox.ID] {
				containerMetrics, err := c.collectContainerMetrics(ctx, container, baseLabels)
				if err != nil {
					switch {
					case errdefs.IsUnavailable(err), errdefs.IsNotFound(err):
						log.G(gctx).WithField("podsandboxid", sandbox.ID).WithField("containerid", container.ID).WithError(err).Error("failed to get container metrics, this is likely a transient error")
						// Don't return error for transient issues, just log and continue
						continue
					case errdefs.IsCanceled(err):
						log.G(gctx).WithField("podsandboxid", sandbox.ID).WithField("containerid", container.ID).WithError(err).Debug("metrics collection cancelled")
						// Return the cancellation error to stop other goroutines
						return err
					default:
						log.G(gctx).WithField("podsandboxid", sandbox.ID).WithField("containerid", container.ID).WithError(err).Error("failed to collect container metrics")
						// Don't return error for individual failures, just log and continue
						continue
					}
				}

				sandboxMetrics.ContainerMetrics = append(sandboxMetrics.ContainerMetrics, containerMetrics)
			}

			// If we already exited the loop because of a canceled request
			// we don't want to write to a potentially closed channel
			if gctx.Err() != nil {
				return gctx.Err()
			}

			metricsMu.Lock()
			podSandboxMetrics = append(podSandboxMetrics, sandboxMetrics)
			metricsMu.Unlock()
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		// log the error and return the metrics that we have collected so far
		log.G(ctx).WithError(err).Error("error during metrics collection, returning partial results")
	}

	return &runtime.ListPodSandboxMetricsResponse{
		PodMetrics: podSandboxMetrics,
	}, nil
}

// collectPodSandboxMetrics collects metrics for a specific pod sandbox
func (c *criService) collectPodSandboxMetrics(ctx context.Context, sandbox sandboxstore.Sandbox, labels []string) (*runtime.PodSandboxMetrics, error) {
	podMetrics := &runtime.PodSandboxMetrics{
		PodSandboxId: sandbox.Metadata.ID,
	}

	// container and image label is set to "" as these are pod level metrics
	podLabels := slices.Concat(labels, []string{"", sandbox.ID, ""})
	timestamp := time.Now().UnixNano()

	// Collect pod-level network metrics
	if sandbox.NetNSPath != "" {
		linkStats, err := getContainerNetIO(ctx, sandbox.NetNSPath)
		if err != nil {
			return nil, err
		}

		podNetworkLabels := slices.Concat(podLabels, []string{defaultIfName})
		podMetrics.Metrics = append(podMetrics.Metrics,
			counter(containerNetworkReceiveBytesTotal, timestamp, podNetworkLabels, linkStats.RxBytes),
			counter(containerNetworkReceivePacketsTotal, timestamp, podNetworkLabels, linkStats.RxPackets),
			counter(containerNetworkReceivePacketsDroppedTotal, timestamp, podNetworkLabels, linkStats.RxDropped),
			counter(containerNetworkReceiveErrorsTotal, timestamp, podNetworkLabels, linkStats.RxErrors),
			counter(containerNetworkTransmitBytesTotal, timestamp, podNetworkLabels, linkStats.TxBytes),
			counter(containerNetworkTransmitPacketsTotal, timestamp, podNetworkLabels, linkStats.TxPackets),
			counter(containerNetworkTransmitPacketsDroppedTotal, timestamp, podNetworkLabels, linkStats.TxDropped),
			counter(containerNetworkTransmitErrorsTotal, timestamp, podNetworkLabels, linkStats.TxErrors),
		)
	}

	return podMetrics, nil
}

func (c *criService) collectContainerMetrics(ctx context.Context, container containerstore.Container, labels []string) (*runtime.ContainerMetrics, error) {
	task, err := container.Container.Task(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get task for container %s: %w", container.ID, err)
	}

	taskMetrics, err := task.Metrics(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get metrics for container %s: %w", container.ID, err)
	}

	taskMetricsAny, err := typeurl.UnmarshalAny(taskMetrics.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal metrics for container %s: %w", container.ID, err)
	}

	var metrics cgroupMetrics
	switch v := taskMetricsAny.(type) {
	case *cg1.Metrics:
		metrics.v1 = v
	case *cg2.Metrics:
		metrics.v2 = v
	default:
		return nil, fmt.Errorf("unexpected metrics type: %T from %s", taskMetricsAny, reflect.TypeOf(taskMetricsAny).Elem().PkgPath())
	}

	containerLabels := slices.Concat(labels, []string{container.Config.Metadata.Name, container.ID, container.Config.Image.UserSpecifiedImage})
	timestamp := time.Now().UnixNano()

	containerMetrics := &runtime.ContainerMetrics{
		ContainerId: container.Metadata.ID,
	}

	// miscellaneous metrics
	containerMetrics.Metrics = append(containerMetrics.Metrics,
		gauge(containerLastSeen, timestamp, containerLabels, uint64(time.Now().Unix())),
		gauge(containerStartTimeSeconds, timestamp, containerLabels, uint64(container.Status.Get().StartedAt/int64(time.Second))),
	)

	cpuMetrics, err := c.extractCPUMetrics(metrics, containerLabels, timestamp)
	if err != nil {
		log.G(ctx).WithField("containerid", container.ID).WithError(err).Info("failed to extract CPU metrics")
	}
	containerMetrics.Metrics = append(containerMetrics.Metrics, cpuMetrics...)

	memoryMetrics, err := c.extractMemoryMetrics(metrics, containerLabels, timestamp)
	if err != nil {
		log.G(ctx).WithField("containerid", container.ID).WithError(err).Info("failed to extract memory metrics")
	}
	containerMetrics.Metrics = append(containerMetrics.Metrics, memoryMetrics...)

	diskMetrics, err := c.extractDiskIOMetrics(metrics, containerLabels, timestamp)
	if err != nil {
		log.G(ctx).WithField("containerid", container.ID).WithError(err).Info("failed to extract disk metrics")
	}
	containerMetrics.Metrics = append(containerMetrics.Metrics, diskMetrics...)

	fsMetrics, err := c.extractFilesystemMetrics(ctx, container, containerLabels, timestamp)
	if err != nil {
		log.G(ctx).WithField("containerid", container.ID).WithError(err).Info("failed to extract filesystem metrics")
	}
	containerMetrics.Metrics = append(containerMetrics.Metrics, fsMetrics...)

	processMetrics, err := c.extractProcessMetrics(ctx, task, metrics, containerLabels, timestamp)
	if err != nil {
		log.G(ctx).WithField("containerid", container.ID).WithError(err).Info("failed to extract process metrics")
	}
	containerMetrics.Metrics = append(containerMetrics.Metrics, processMetrics...)

	containerSpecMetrics, err := c.extractContainerSpecMetrics(ctx, task, containerLabels, timestamp)
	if err != nil {
		log.G(ctx).WithField("containerid", container.ID).WithError(err).Info("failed to extract container spec metrics")
	}
	containerMetrics.Metrics = append(containerMetrics.Metrics, containerSpecMetrics...)

	return containerMetrics, nil
}

func (c *criService) extractCPUMetrics(stats cgroupMetrics, labels []string, timestamp int64) ([]*runtime.Metric, error) {
	var metrics []*runtime.Metric

	switch {
	case stats.v1 != nil:
		s := stats.v1

		if s.CPU != nil && s.CPU.Usage != nil {
			metrics = append(metrics, counter(containerCPUUsageSecondsTotal, timestamp, labels, s.CPU.Usage.Total/uint64(time.Second)))

			metrics = append(metrics,
				counter(containerCPUUserSecondsTotal, timestamp, labels, s.CPU.Usage.User/uint64(time.Second)),
				counter(containerCPUSystemSecondsTotal, timestamp, labels, s.CPU.Usage.Kernel/uint64(time.Second)),
			)

			if s.CPU.Throttling != nil {
				metrics = append(metrics,
					counter(containerCPUCfsPeriodsTotal, timestamp, labels, s.CPU.Throttling.Periods),
					counter(containerCPUCfsThrottledPeriodsTotal, timestamp, labels, s.CPU.Throttling.ThrottledPeriods),
					counter(containerCPUCfsThrottledSecondsTotal, timestamp, labels, s.CPU.Throttling.ThrottledTime/uint64(time.Second)),
				)
			}
		}

	case stats.v2 != nil:
		s := stats.v2

		if s.CPU != nil {
			metrics = append(metrics, counter(containerCPUUsageSecondsTotal, timestamp, labels, s.CPU.UsageUsec*1000/uint64(time.Second)))

			metrics = append(metrics,
				counter(containerCPUUserSecondsTotal, timestamp, labels, s.CPU.UserUsec*1000/uint64(time.Second)),
				counter(containerCPUSystemSecondsTotal, timestamp, labels, s.CPU.SystemUsec*1000/uint64(time.Second)),
			)

			// Always include CFS throttling metrics, even if zero
			metrics = append(metrics,
				counter(containerCPUCfsPeriodsTotal, timestamp, labels, s.CPU.NrPeriods),
				counter(containerCPUCfsThrottledPeriodsTotal, timestamp, labels, s.CPU.NrThrottled),
				counter(containerCPUCfsThrottledSecondsTotal, timestamp, labels, s.CPU.ThrottledUsec*1000/uint64(time.Second)),
			)
		}
	}

	return metrics, nil
}

func (c *criService) extractMemoryMetrics(stats cgroupMetrics, labels []string, timestamp int64) ([]*runtime.Metric, error) {
	var metrics []*runtime.Metric

	switch {
	case stats.v1 != nil:
		s := stats.v1

		if s.Memory != nil {
			if s.Memory.Usage != nil {
				metrics = append(metrics,
					gauge(containerMemoryUsageBytes, timestamp, labels, s.Memory.Usage.Usage),
					gauge(containerMemoryWorkingSetBytes, timestamp, labels, getWorkingSet(s.Memory)),
					gauge(containerMemoryMaxUsageBytes, timestamp, labels, s.Memory.Usage.Max),
				)
			}

			metrics = append(metrics,
				gauge(containerMemoryRss, timestamp, labels, s.Memory.TotalRSS),
				gauge(containerMemoryCache, timestamp, labels, s.Memory.TotalCache),
				gauge(containerMemoryMappedFile, timestamp, labels, s.Memory.TotalMappedFile),
				gauge(containerMemoryTotalActiveFileBytes, timestamp, labels, s.Memory.TotalActiveFile),
				gauge(containerMemoryTotalInactiveFileBytes, timestamp, labels, s.Memory.TotalInactiveFile),
				counter(containerMemoryFailuresTotal, timestamp, slices.Concat(labels, []string{"pgfault", "container"}), s.Memory.PgFault),
				counter(containerMemoryFailuresTotal, timestamp, slices.Concat(labels, []string{"pgmajfault", "container"}), s.Memory.PgMajFault),
			)

			if s.Memory.Kernel != nil {
				metrics = append(metrics, gauge(containerMemoryKernelUsage, timestamp, labels, s.Memory.Kernel.Usage))
			}

			if s.Memory.Swap != nil {
				metrics = append(metrics, gauge(containerMemorySwap, timestamp, labels, s.Memory.Swap.Usage))
			}

			if s.Memory.Usage != nil {
				metrics = append(metrics, counter(containerMemoryFailcnt, timestamp, labels, s.Memory.Usage.Failcnt))
			}

		}

		if s.MemoryOomControl != nil {
			metrics = append(metrics, counter(containerOomEventsTotal, timestamp, labels, s.MemoryOomControl.GetOomKill()))
		}

	case stats.v2 != nil:
		s := stats.v2

		if s.Memory != nil {
			metrics = append(metrics,
				gauge(containerMemoryUsageBytes, timestamp, labels, s.Memory.Usage),
				gauge(containerMemoryMaxUsageBytes, timestamp, labels, s.Memory.UsageLimit),
				gauge(containerMemoryWorkingSetBytes, timestamp, labels, getWorkingSetV2(s.Memory)),
				gauge(containerMemoryRss, timestamp, labels, s.Memory.Anon),
				gauge(containerMemoryCache, timestamp, labels, s.Memory.File),
				gauge(containerMemoryKernelUsage, timestamp, labels, s.Memory.KernelStack),
				gauge(containerMemoryMappedFile, timestamp, labels, s.Memory.FileMapped),
				gauge(containerMemorySwap, timestamp, labels, s.Memory.SwapUsage),
				counter(containerMemoryFailcnt, timestamp, labels, 0), // cgroups v2 doesn't expose failcnt, provide 0
				gauge(containerMemoryTotalActiveFileBytes, timestamp, labels, s.Memory.ActiveFile),
				gauge(containerMemoryTotalInactiveFileBytes, timestamp, labels, s.Memory.InactiveFile),
				// TODO how to get hierarchical ?
				counter(containerMemoryFailuresTotal, timestamp, slices.Concat(labels, []string{"pgfault", "container"}), s.Memory.Pgfault),
				counter(containerMemoryFailuresTotal, timestamp, slices.Concat(labels, []string{"pgmajfault", "container"}), s.Memory.Pgmajfault),
			)

		}

		if s.MemoryEvents != nil {
			metrics = append(metrics, counter(containerOomEventsTotal, timestamp, labels, s.MemoryEvents.GetOomKill()))
		}
	}

	return metrics, nil
}

func (c *criService) extractDiskIOMetrics(stats cgroupMetrics, labels []string, timestamp int64) ([]*runtime.Metric, error) {
	var metrics []*runtime.Metric

	switch {
	case stats.v1 != nil:
		s := stats.v1

		if s.Blkio != nil {
			// Process blkio device usage stats
			for _, entry := range s.Blkio.IoServiceBytesRecursive {
				metrics = append(metrics, counter(containerBlkioDeviceUsageTotal, timestamp, slices.Concat(labels, []string{entry.Device, strconv.FormatUint(entry.Major, 10), strconv.FormatUint(entry.Minor, 10), entry.Op}), entry.Value))
			}

			// Process filesystem read/write stats
			for _, entry := range s.Blkio.IoServicedRecursive {
				switch strings.ToLower(entry.Op) {
				case "read":
					metrics = append(metrics, counter(containerFsReadsTotal, timestamp, slices.Concat(labels, []string{entry.Device}), entry.Value))
				case "write":
					metrics = append(metrics, counter(containerFsWritesTotal, timestamp, slices.Concat(labels, []string{entry.Device}), entry.Value))
				}
			}

			// Process filesystem bytes read/written
			for _, entry := range s.Blkio.IoServiceBytesRecursive {
				switch strings.ToLower(entry.Op) {
				case "read":
					metrics = append(metrics, counter(containerFsReadsBytesTotal, timestamp, slices.Concat(labels, []string{entry.Device}), entry.Value))
				case "write":
					metrics = append(metrics, counter(containerFsWritesBytesTotal, timestamp, slices.Concat(labels, []string{entry.Device}), entry.Value))
				}
			}
		}

	case stats.v2 != nil:
		s := stats.v2

		if s.Io != nil {
			// Process cgroups v2 I/O stats
			for _, entry := range s.Io.Usage {
				device := fmt.Sprintf("%d:%d", entry.Major, entry.Minor)
				metrics = append(metrics,
					counter(containerFsReadsBytesTotal, timestamp, slices.Concat(labels, []string{device}), entry.Rbytes),
					counter(containerFsWritesBytesTotal, timestamp, slices.Concat(labels, []string{device}), entry.Wbytes),
					counter(containerFsReadsTotal, timestamp, slices.Concat(labels, []string{device}), entry.Rios),
					counter(containerFsWritesTotal, timestamp, slices.Concat(labels, []string{device}), entry.Wios),
				)

			}
		}
	}

	return metrics, nil
}

func (c *criService) extractProcessMetrics(ctx context.Context, task containerd.Task, stats cgroupMetrics, labels []string, timestamp int64) ([]*runtime.Metric, error) {
	var metrics []*runtime.Metric

	switch {
	case stats.v1 != nil:
		s := stats.v1

		if s.Pids != nil {
			metrics = append(metrics,
				gauge(containerProcesses, timestamp, labels, s.Pids.Current),
				gauge(containerThreadsMax, timestamp, labels, s.Pids.Limit),
				gauge(containerThreads, timestamp, labels, s.Pids.Current),
			)
		}

	case stats.v2 != nil:
		s := stats.v2

		if s.Pids != nil {
			metrics = append(metrics,
				gauge(containerProcesses, timestamp, labels, s.Pids.Current),
				gauge(containerThreadsMax, timestamp, labels, s.Pids.Limit),
				gauge(containerThreads, timestamp, labels, s.Pids.Current),
			)
		}
	}

	taskSpec, err := task.Spec(ctx)
	if err != nil {
		return metrics, fmt.Errorf("failed to get task spec: %w", err)
	}

	if taskSpec != nil && taskSpec.Process != nil {
		for _, rlimit := range taskSpec.Process.Rlimits {
			metrics = append(metrics, gauge(containerUlimitsSoft, timestamp, slices.Concat(labels, []string{rlimit.Type}), rlimit.Soft))
		}
	}

	// Collect file descriptor and socket metrics
	pidsInfo, err := task.Pids(ctx)
	if err != nil {
		return metrics, fmt.Errorf("failed to get container PIDs for FD/socket metrics: %w", err)
	}

	pids := make([]int, 0, len(pidsInfo))
	for _, pidInfo := range pidsInfo {
		pids = append(pids, int(pidInfo.Pid))
	}

	fdCount, socketCount, err := c.getContainerProcessDescriptorCount(ctx, pids)
	if err != nil {
		return metrics, fmt.Errorf("failed to count file descriptors and sockets: %w", err)
	}

	metrics = append(metrics,
		gauge(containerFileDescriptors, timestamp, labels, fdCount),
		gauge(containerSockets, timestamp, labels, socketCount),
	)

	return metrics, nil
}

func (c *criService) extractContainerSpecMetrics(ctx context.Context, task containerd.Task, labels []string, timestamp int64) ([]*runtime.Metric, error) {
	taskSpec, err := task.Spec(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get task spec: %w", err)
	}

	// No resource limits are set. No error but no metrics too.
	if taskSpec.Linux == nil || taskSpec.Linux.Resources == nil {
		return nil, nil
	}
	resource := taskSpec.Linux.Resources

	var metrics []*runtime.Metric
	if cpuResource := resource.CPU; cpuResource != nil {
		if cpuResource.Period != nil {
			metrics = append(metrics, gauge(containerSpecCPUPeriod, timestamp, labels, *cpuResource.Period))
		}
		if cpuResource.Shares != nil {
			metrics = append(metrics, gauge(containerSpecCPUShares, timestamp, labels, *cpuResource.Shares))
		}
	}

	if memoryResource := resource.Memory; memoryResource != nil {
		if memoryResource.Limit != nil {
			metrics = append(metrics, gauge(containerSpecMemoryLimitBytes, timestamp, labels, uint64(*memoryResource.Limit)))
		}
		if memoryResource.Reservation != nil {
			metrics = append(metrics, gauge(containerSpecMemoryReservationLimitBytes, timestamp, labels, uint64(*memoryResource.Reservation)))
		}
		if memoryResource.Swap != nil {
			metrics = append(metrics, gauge(containerSpecMemorySwapLimitBytes, timestamp, labels, uint64(*memoryResource.Swap)))
		}
	}
	return metrics, nil
}

// findContainerTaskRootfs tries to find the rootfs of a container,
// but it's inherently broken as this info should be provided by
// the task itself. Sadly taskSpec.Root.Path is filled with a
// relative path instead of the spec required absolute path.
// TODO: Fix this mess
func (c *criService) findContainerTaskRootfs(container containers.Container) (string, error) {
	// TODO: Can't we just read the used plugin from the container?
	var pluginPath string
	switch container.Runtime.Name {
	case "runc":
		pluginPath = string(plugins.RuntimePlugin) + ".linux"
	default:
		pluginPath = string(plugins.RuntimePluginV2) + ".task"
	}

	// For containerd, the typical bundle path structure is:
	// {stateDir/../}/{plugin}/{namespace}/{id}
	// The rootfs is typically at: {bundle}/rootfs
	bundlePath := filepath.Join(
		filepath.Join(c.config.StateDir, "../"),
		pluginPath,
		c.nri.GetName(),
		container.ID,
	)
	rootfsPath := filepath.Join(bundlePath, "rootfs")

	// Check if the path exists
	if _, err := os.Stat(rootfsPath); err == nil {
		return rootfsPath, nil
	}

	return "", errors.New("could not determine container root path")
}

func (c *criService) extractFilesystemMetrics(ctx context.Context, container containerstore.Container, labels []string, timestamp int64) ([]*runtime.Metric, error) {
	ctrInfo, err := container.Container.Info(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get container info: %w", err)
	}

	rootfs, err := c.findContainerTaskRootfs(ctrInfo)
	if err != nil {
		return nil, err
	}

	var stat syscall.Statfs_t
	if err := syscall.Statfs(rootfs, &stat); err != nil {
		return nil, fmt.Errorf("failed to get root filesystem info: %w", err)
	}

	var (
		totalBytes     = stat.Blocks * uint64(stat.Bsize)
		availableBytes = stat.Bavail * uint64(stat.Bsize)
		usedBytes      = totalBytes - availableBytes
		limit          = stat.Blocks * uint64(stat.Bsize)
		inodes         = stat.Files
		inodesFree     = stat.Ffree
	)

	snapshotUsage, err := c.client.SnapshotService(ctrInfo.Snapshotter).Usage(ctx, ctrInfo.SnapshotKey)
	if err == nil {
		usedBytes = uint64(snapshotUsage.Size)
		inodes = uint64(snapshotUsage.Inodes)
	}

	// TODO: These are wrong, but I (fionera) don't know the internals enough to correctly fetch
	// the correct device metrics.
	metrics := []*runtime.Metric{
		gauge(containerFsUsageBytes, timestamp, slices.Concat(labels, []string{"/"}), usedBytes),
		gauge(containerFsLimitBytes, timestamp, slices.Concat(labels, []string{"/"}), limit),
		gauge(containerFsInodesTotal, timestamp, slices.Concat(labels, []string{"/"}), inodes),
		gauge(containerFsInodesFree, timestamp, slices.Concat(labels, []string{"/"}), inodesFree),
	}

	return metrics, nil
}

// getContainerProcessDescriptorCount gets the total no of fds and sockets in a pid
// Referenced from https://github.com/google/cadvisor/blob/master/container/libcontainer/handler.go
func (c *criService) getContainerProcessDescriptorCount(ctx context.Context, pids []int) (fdCount, socketCount uint64, err error) {
	for _, pid := range pids {
		fdDir := path.Join("/proc", strconv.Itoa(pid), "fd")
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			log.G(ctx).WithField("pid", pid).WithError(err).Debug("failed to read fd directory for PID")
			continue
		}
		fdCount += uint64(len(fds))

		for _, fd := range fds {
			fdPath := filepath.Join(fdDir, fd.Name())
			linkTarget, err := os.Readlink(fdPath)
			if err != nil {
				log.G(ctx).WithField("fd", fdPath).WithError(err).Debug("error while reading fd link")
				continue
			}

			// check if the fd is a socket
			if strings.HasPrefix(linkTarget, "socket") {
				socketCount++
			}
		}
	}

	return fdCount, socketCount, nil
}

// gauge returns a gauge sample of desc with the given labels and value.
func gauge(desc *runtime.MetricDescriptor, timestamp int64, labels []string, value uint64) *runtime.Metric {
	return newUInt64Metric(desc, runtime.MetricType_GAUGE, timestamp, labels, value)
}

// counter returns a counter sample of desc with the given labels and value.
func counter(desc *runtime.MetricDescriptor, timestamp int64, labels []string, value uint64) *runtime.Metric {
	return newUInt64Metric(desc, runtime.MetricType_COUNTER, timestamp, labels, value)
}

func newUInt64Metric(desc *runtime.MetricDescriptor, metricType runtime.MetricType, timestamp int64, labels []string, value uint64) *runtime.Metric {
	return &runtime.Metric{
		Name:        desc.Name,
		Timestamp:   timestamp,
		MetricType:  metricType,
		LabelValues: labels,
		Value:       &runtime.UInt64Value{Value: value},
	}
}
