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

package instrument

import (
	"context"

	"github.com/containerd/errdefs"
	"github.com/containerd/errdefs/pkg/errgrpc"
	"github.com/containerd/log"
	"google.golang.org/grpc"
	runtime "k8s.io/cri-api/pkg/apis/runtime/v1"

	"github.com/containerd/containerd/v2/pkg/tracing"

	ctrdutil "github.com/containerd/containerd/v2/internal/cri/util"
)

// criService is an CRI server dependency to be wrapped with instrumentation.
type criService interface {
	GRPCServices

	IsInitialized() bool
}

// GRPCServices are all the grpc services provided by cri containerd.
type GRPCServices interface {
	runtime.RuntimeServiceServer
	runtime.ImageServiceServer
}

// instrumentedService wraps service with containerd namespace and logs.
type instrumentedService struct {
	runtime.UnimplementedRuntimeServiceServer
	runtime.UnimplementedImageServiceServer

	c criService
}

func NewService(c criService) GRPCServices {
	return &instrumentedService{c: c}
}

// checkInitialized returns error if the server is not fully initialized.
// GRPC service request handlers should return error before server is fully
// initialized.
// NOTE(random-liu): All following functions MUST check initialized at the beginning.
func (in *instrumentedService) checkInitialized() error {
	if in.c.IsInitialized() {
		return nil
	}
	return errgrpc.ToGRPCf(errdefs.ErrUnavailable, "server is not initialized yet")
}

// logLine is a log message and the level to write it at.
type logLine struct {
	level  log.Level
	format string
	args   []any
}

func infof(format string, args ...any) logLine  { return logLine{log.InfoLevel, format, args} }
func debugf(format string, args ...any) logLine { return logLine{log.DebugLevel, format, args} }
func tracef(format string, args ...any) logLine { return logLine{log.TraceLevel, format, args} }
func errorf(format string, args ...any) logLine { return logLine{log.ErrorLevel, format, args} }

// write logs l to e. A zero logLine writes nothing.
func (l logLine) write(e *log.Entry) {
	if l.format != "" {
		e.Logf(l.level, l.format, l.args...)
	}
}

// spanMode selects whether call records the RPC error on the request span.
type spanMode bool

const (
	untraced spanMode = false
	traced   spanMode = true
)

// call serves one unary RPC with fn. It fails if the service is not
// initialized, logs start, calls fn with the containerd namespace in the
// context, and converts the returned error to a gRPC error. It then logs
// failed with that error, or done with the response, and records the error
// on the span when mode is traced.
func call[Req, Res any](ctx context.Context, in *instrumentedService, r Req, fn func(context.Context, Req) (Res, error),
	mode spanMode, start, failed logLine, done func(Res) logLine) (res Res, err error) {
	if err := in.checkInitialized(); err != nil {
		return res, err
	}
	start.write(log.G(ctx))
	defer func() {
		if err != nil {
			failed.write(log.G(ctx).WithError(err))
		} else {
			done(res).write(log.G(ctx))
		}
		if mode == traced {
			tracing.SpanFromContext(ctx).RecordError(err)
		}
	}()
	res, err = fn(ctrdutil.WithNamespace(ctx), r)
	return res, errgrpc.ToGRPC(err)
}

// sanitized wraps fn to remove sensitive information from its error, which
// call then logs and returns.
func sanitized[Req, Res any](fn func(context.Context, Req) (Res, error)) func(context.Context, Req) (Res, error) {
	return func(ctx context.Context, r Req) (Res, error) {
		res, err := fn(ctx, r)
		if err != nil {
			err = ctrdutil.SanitizeError(err)
		}
		return res, err
	}
}

func (in *instrumentedService) RunPodSandbox(ctx context.Context, r *runtime.RunPodSandboxRequest) (*runtime.RunPodSandboxResponse, error) {
	return call(ctx, in, r, in.c.RunPodSandbox, traced,
		infof("RunPodSandbox for %+v", r.GetConfig().GetMetadata()),
		errorf("RunPodSandbox for %+v failed, error", r.GetConfig().GetMetadata()),
		func(res *runtime.RunPodSandboxResponse) logLine {
			return infof("RunPodSandbox for %+v returns sandbox id %q", r.GetConfig().GetMetadata(), res.GetPodSandboxId())
		})
}

func (in *instrumentedService) ListPodSandbox(ctx context.Context, r *runtime.ListPodSandboxRequest) (*runtime.ListPodSandboxResponse, error) {
	return call(ctx, in, r, in.c.ListPodSandbox, untraced,
		tracef("ListPodSandbox with filter %+v", r.GetFilter()),
		errorf("ListPodSandbox failed"),
		func(res *runtime.ListPodSandboxResponse) logLine {
			return tracef("ListPodSandbox returns pod sandboxes %+v", res.GetItems())
		})
}

func (in *instrumentedService) PodSandboxStatus(ctx context.Context, r *runtime.PodSandboxStatusRequest) (*runtime.PodSandboxStatusResponse, error) {
	return call(ctx, in, r, in.c.PodSandboxStatus, untraced,
		tracef("PodSandboxStatus for %q", r.GetPodSandboxId()),
		errorf("PodSandboxStatus for %q failed", r.GetPodSandboxId()),
		func(res *runtime.PodSandboxStatusResponse) logLine {
			return tracef("PodSandboxStatus for %q returns status %+v", r.GetPodSandboxId(), res.GetStatus())
		})
}

func (in *instrumentedService) StopPodSandbox(ctx context.Context, r *runtime.StopPodSandboxRequest) (*runtime.StopPodSandboxResponse, error) {
	return call(ctx, in, r, in.c.StopPodSandbox, traced,
		infof("StopPodSandbox for %q", r.GetPodSandboxId()),
		errorf("StopPodSandbox for %q failed", r.GetPodSandboxId()),
		func(*runtime.StopPodSandboxResponse) logLine {
			return infof("StopPodSandbox for %q returns successfully", r.GetPodSandboxId())
		})
}

func (in *instrumentedService) RemovePodSandbox(ctx context.Context, r *runtime.RemovePodSandboxRequest) (*runtime.RemovePodSandboxResponse, error) {
	return call(ctx, in, r, in.c.RemovePodSandbox, traced,
		infof("RemovePodSandbox for %q", r.GetPodSandboxId()),
		errorf("RemovePodSandbox for %q failed", r.GetPodSandboxId()),
		func(*runtime.RemovePodSandboxResponse) logLine {
			return infof("RemovePodSandbox %q returns successfully", r.GetPodSandboxId())
		})
}

func (in *instrumentedService) PortForward(ctx context.Context, r *runtime.PortForwardRequest) (*runtime.PortForwardResponse, error) {
	return call(ctx, in, r, in.c.PortForward, untraced,
		infof("Portforward for %q port %v", r.GetPodSandboxId(), r.GetPort()),
		errorf("Portforward for %q failed", r.GetPodSandboxId()),
		func(res *runtime.PortForwardResponse) logLine {
			return infof("Portforward for %q returns URL %q", r.GetPodSandboxId(), res.GetUrl())
		})
}

func (in *instrumentedService) CreateContainer(ctx context.Context, r *runtime.CreateContainerRequest) (*runtime.CreateContainerResponse, error) {
	return call(ctx, in, r, in.c.CreateContainer, traced,
		infof("CreateContainer within sandbox %q for container %+v",
			r.GetPodSandboxId(), r.GetConfig().GetMetadata()),
		errorf("CreateContainer within sandbox %q for %+v failed",
			r.GetPodSandboxId(), r.GetConfig().GetMetadata()),
		func(res *runtime.CreateContainerResponse) logLine {
			return infof("CreateContainer within sandbox %q for %+v returns container id %q",
				r.GetPodSandboxId(), r.GetConfig().GetMetadata(), res.GetContainerId())
		})
}

func (in *instrumentedService) StartContainer(ctx context.Context, r *runtime.StartContainerRequest) (*runtime.StartContainerResponse, error) {
	return call(ctx, in, r, in.c.StartContainer, traced,
		infof("StartContainer for %q", r.GetContainerId()),
		errorf("StartContainer for %q failed", r.GetContainerId()),
		func(*runtime.StartContainerResponse) logLine {
			return infof("StartContainer for %q returns successfully", r.GetContainerId())
		})
}

func (in *instrumentedService) ListContainers(ctx context.Context, r *runtime.ListContainersRequest) (*runtime.ListContainersResponse, error) {
	return call(ctx, in, r, in.c.ListContainers, untraced,
		tracef("ListContainers with filter %+v", r.GetFilter()),
		errorf("ListContainers with filter %+v failed", r.GetFilter()),
		func(res *runtime.ListContainersResponse) logLine {
			return tracef("ListContainers with filter %+v returns containers %+v",
				r.GetFilter(), res.GetContainers())
		})
}

func (in *instrumentedService) ContainerStatus(ctx context.Context, r *runtime.ContainerStatusRequest) (*runtime.ContainerStatusResponse, error) {
	return call(ctx, in, r, in.c.ContainerStatus, untraced,
		tracef("ContainerStatus for %q", r.GetContainerId()),
		errorf("ContainerStatus for %q failed", r.GetContainerId()),
		func(res *runtime.ContainerStatusResponse) logLine {
			return tracef("ContainerStatus for %q returns status %+v", r.GetContainerId(), res.GetStatus())
		})
}

func (in *instrumentedService) StopContainer(ctx context.Context, r *runtime.StopContainerRequest) (*runtime.StopContainerResponse, error) {
	return call(ctx, in, r, in.c.StopContainer, traced,
		infof("StopContainer for %q with timeout %d (s)", r.GetContainerId(), r.GetTimeout()),
		errorf("StopContainer for %q failed", r.GetContainerId()),
		func(*runtime.StopContainerResponse) logLine {
			return infof("StopContainer for %q returns successfully", r.GetContainerId())
		})
}

func (in *instrumentedService) RemoveContainer(ctx context.Context, r *runtime.RemoveContainerRequest) (*runtime.RemoveContainerResponse, error) {
	return call(ctx, in, r, in.c.RemoveContainer, traced,
		infof("RemoveContainer for %q", r.GetContainerId()),
		errorf("RemoveContainer for %q failed", r.GetContainerId()),
		func(*runtime.RemoveContainerResponse) logLine {
			return infof("RemoveContainer for %q returns successfully", r.GetContainerId())
		})
}

func (in *instrumentedService) ExecSync(ctx context.Context, r *runtime.ExecSyncRequest) (*runtime.ExecSyncResponse, error) {
	return call(ctx, in, r, in.c.ExecSync, traced,
		debugf("ExecSync for %q with command %+v and timeout %d (s)", r.GetContainerId(), r.GetCmd(), r.GetTimeout()),
		errorf("ExecSync for %q failed", r.GetContainerId()),
		func(res *runtime.ExecSyncResponse) logLine {
			return tracef("ExecSync for %q returns with exit code %d", r.GetContainerId(), res.GetExitCode())
		})
}

func (in *instrumentedService) Exec(ctx context.Context, r *runtime.ExecRequest) (*runtime.ExecResponse, error) {
	return call(ctx, in, r, in.c.Exec, traced,
		debugf("Exec for %q with command %+v, tty %v and stdin %v",
			r.GetContainerId(), r.GetCmd(), r.GetTty(), r.GetStdin()),
		errorf("Exec for %q failed", r.GetContainerId()),
		func(res *runtime.ExecResponse) logLine {
			return debugf("Exec for %q returns URL %q", r.GetContainerId(), res.GetUrl())
		})
}

func (in *instrumentedService) Attach(ctx context.Context, r *runtime.AttachRequest) (*runtime.AttachResponse, error) {
	return call(ctx, in, r, in.c.Attach, traced,
		debugf("Attach for %q with tty %v and stdin %v", r.GetContainerId(), r.GetTty(), r.GetStdin()),
		errorf("Attach for %q failed", r.GetContainerId()),
		func(res *runtime.AttachResponse) logLine {
			return debugf("Attach for %q returns URL %q", r.GetContainerId(), res.Url)
		})
}

func (in *instrumentedService) UpdateContainerResources(ctx context.Context, r *runtime.UpdateContainerResourcesRequest) (*runtime.UpdateContainerResourcesResponse, error) {
	return call(ctx, in, r, in.c.UpdateContainerResources, untraced,
		infof("UpdateContainerResources for %q with Linux: %+v / Windows: %+v", r.GetContainerId(), r.GetLinux(), r.GetWindows()),
		errorf("UpdateContainerResources for %q failed", r.GetContainerId()),
		func(*runtime.UpdateContainerResourcesResponse) logLine {
			return infof("UpdateContainerResources for %q returns successfully", r.GetContainerId())
		})
}

func (in *instrumentedService) PullImage(ctx context.Context, r *runtime.PullImageRequest) (*runtime.PullImageResponse, error) {
	return call(ctx, in, r, sanitized(in.c.PullImage), traced,
		infof("PullImage %q", r.GetImage().GetImage()),
		errorf("PullImage %q failed", r.GetImage().GetImage()),
		func(res *runtime.PullImageResponse) logLine {
			return infof("PullImage %q returns image reference %q",
				r.GetImage().GetImage(), res.GetImageRef())
		})
}

func (in *instrumentedService) ListImages(ctx context.Context, r *runtime.ListImagesRequest) (*runtime.ListImagesResponse, error) {
	return call(ctx, in, r, sanitized(in.c.ListImages), untraced,
		tracef("ListImages with filter %+v", r.GetFilter()),
		errorf("ListImages with filter %+v failed", r.GetFilter()),
		func(res *runtime.ListImagesResponse) logLine {
			return tracef("ListImages with filter %+v returns image list %+v",
				r.GetFilter(), res.GetImages())
		})
}

func (in *instrumentedService) ImageStatus(ctx context.Context, r *runtime.ImageStatusRequest) (*runtime.ImageStatusResponse, error) {
	return call(ctx, in, r, sanitized(in.c.ImageStatus), untraced,
		tracef("ImageStatus for %q", r.GetImage().GetImage()),
		errorf("ImageStatus for %q failed", r.GetImage().GetImage()),
		func(res *runtime.ImageStatusResponse) logLine {
			return tracef("ImageStatus for %q returns image status %+v",
				r.GetImage().GetImage(), res.GetImage())
		})
}

func (in *instrumentedService) RemoveImage(ctx context.Context, r *runtime.RemoveImageRequest) (*runtime.RemoveImageResponse, error) {
	return call(ctx, in, r, sanitized(in.c.RemoveImage), traced,
		infof("RemoveImage %q", r.GetImage().GetImage()),
		errorf("RemoveImage %q failed", r.GetImage().GetImage()),
		func(*runtime.RemoveImageResponse) logLine {
			return infof("RemoveImage %q returns successfully", r.GetImage().GetImage())
		})
}

func (in *instrumentedService) ImageFsInfo(ctx context.Context, r *runtime.ImageFsInfoRequest) (*runtime.ImageFsInfoResponse, error) {
	return call(ctx, in, r, in.c.ImageFsInfo, untraced,
		tracef("ImageFsInfo"),
		errorf("ImageFsInfo failed"),
		func(res *runtime.ImageFsInfoResponse) logLine {
			return tracef("ImageFsInfo returns filesystem info %+v", res.ImageFilesystems)
		})
}

func (in *instrumentedService) PodSandboxStats(ctx context.Context, r *runtime.PodSandboxStatsRequest) (*runtime.PodSandboxStatsResponse, error) {
	return call(ctx, in, r, in.c.PodSandboxStats, untraced,
		tracef("PodSandboxStats for %q", r.GetPodSandboxId()),
		errorf("PodSandboxStats for %q failed", r.GetPodSandboxId()),
		func(res *runtime.PodSandboxStatsResponse) logLine {
			return tracef("PodSandboxStats for %q returns stats %+v", r.GetPodSandboxId(), res.GetStats())
		})
}

func (in *instrumentedService) ContainerStats(ctx context.Context, r *runtime.ContainerStatsRequest) (*runtime.ContainerStatsResponse, error) {
	return call(ctx, in, r, in.c.ContainerStats, untraced,
		tracef("ContainerStats for %q", r.GetContainerId()),
		errorf("ContainerStats for %q failed", r.GetContainerId()),
		func(res *runtime.ContainerStatsResponse) logLine {
			return tracef("ContainerStats for %q returns stats %+v", r.GetContainerId(), res.GetStats())
		})
}

func (in *instrumentedService) ListPodSandboxStats(ctx context.Context, r *runtime.ListPodSandboxStatsRequest) (*runtime.ListPodSandboxStatsResponse, error) {
	return call(ctx, in, r, in.c.ListPodSandboxStats, untraced,
		tracef("ListPodSandboxStats with filter %+v", r.GetFilter()),
		errorf("ListPodSandboxStats failed"),
		func(res *runtime.ListPodSandboxStatsResponse) logLine {
			return tracef("ListPodSandboxStats returns stats %+v", res.GetStats())
		})
}

func (in *instrumentedService) ListContainerStats(ctx context.Context, r *runtime.ListContainerStatsRequest) (*runtime.ListContainerStatsResponse, error) {
	return call(ctx, in, r, in.c.ListContainerStats, untraced,
		tracef("ListContainerStats with filter %+v", r.GetFilter()),
		errorf("ListContainerStats failed"),
		func(res *runtime.ListContainerStatsResponse) logLine {
			return tracef("ListContainerStats returns stats %+v", res.GetStats())
		})
}

func (in *instrumentedService) Status(ctx context.Context, r *runtime.StatusRequest) (*runtime.StatusResponse, error) {
	return call(ctx, in, r, in.c.Status, untraced,
		tracef("Status"),
		errorf("Status failed"),
		func(res *runtime.StatusResponse) logLine {
			return tracef("Status returns status %+v", res.GetStatus())
		})
}

func (in *instrumentedService) Version(ctx context.Context, r *runtime.VersionRequest) (*runtime.VersionResponse, error) {
	return call(ctx, in, r, in.c.Version, untraced,
		tracef("Version with client side version %q", r.GetVersion()),
		errorf("Version failed"),
		func(res *runtime.VersionResponse) logLine {
			return tracef("Version returns %+v", res)
		})
}

func (in *instrumentedService) UpdateRuntimeConfig(ctx context.Context, r *runtime.UpdateRuntimeConfigRequest) (*runtime.UpdateRuntimeConfigResponse, error) {
	return call(ctx, in, r, in.c.UpdateRuntimeConfig, untraced,
		debugf("UpdateRuntimeConfig with config %+v", r.GetRuntimeConfig()),
		errorf("UpdateRuntimeConfig failed"),
		func(*runtime.UpdateRuntimeConfigResponse) logLine {
			return debugf("UpdateRuntimeConfig returns successfully")
		})
}

func (in *instrumentedService) ReopenContainerLog(ctx context.Context, r *runtime.ReopenContainerLogRequest) (*runtime.ReopenContainerLogResponse, error) {
	return call(ctx, in, r, in.c.ReopenContainerLog, untraced,
		debugf("ReopenContainerLog for %q", r.GetContainerId()),
		errorf("ReopenContainerLog for %q failed", r.GetContainerId()),
		func(*runtime.ReopenContainerLogResponse) logLine {
			return debugf("ReopenContainerLog for %q returns successfully", r.GetContainerId())
		})
}

func (in *instrumentedService) CheckpointContainer(ctx context.Context, r *runtime.CheckpointContainerRequest) (*runtime.CheckpointContainerResponse, error) {
	return call(ctx, in, r, in.c.CheckpointContainer, untraced,
		logLine{},
		errorf("CheckpointContainer failed, error"),
		func(*runtime.CheckpointContainerResponse) logLine {
			return debugf("CheckpointContainer returns successfully")
		})
}

func (in *instrumentedService) GetContainerEvents(r *runtime.GetEventsRequest, s runtime.RuntimeService_GetContainerEventsServer) (err error) {
	if err := in.checkInitialized(); err != nil {
		return err
	}

	ctx := s.Context()
	defer func() {
		if err != nil {
			log.G(ctx).WithError(err).Errorf("GetContainerEvents failed, error")
		} else {
			log.G(ctx).Debug("GetContainerEvents returns successfully")
		}
	}()

	err = in.c.GetContainerEvents(r, s)
	return errgrpc.ToGRPC(err)
}

func (in *instrumentedService) ListMetricDescriptors(ctx context.Context, r *runtime.ListMetricDescriptorsRequest) (res *runtime.ListMetricDescriptorsResponse, err error) {
	if err := in.checkInitialized(); err != nil {
		return nil, err
	}

	defer func() {
		if err != nil {
			log.G(ctx).WithError(err).Errorf("ListMetricDescriptors failed, error")
		} else {
			log.G(ctx).Trace("ListMetricDescriptors returns successfully")
		}
	}()

	res, err = in.c.ListMetricDescriptors(ctx, r)
	return res, errgrpc.ToGRPC(err)
}

func (in *instrumentedService) ListPodSandboxMetrics(ctx context.Context, r *runtime.ListPodSandboxMetricsRequest) (res *runtime.ListPodSandboxMetricsResponse, err error) {
	if err := in.checkInitialized(); err != nil {
		return nil, err
	}

	defer func() {
		if err != nil {
			log.G(ctx).WithError(err).Errorf("ListPodSandboxMetrics failed, error")
		} else {
			log.G(ctx).Trace("ListPodSandboxMetrics returns successfully")
		}
	}()

	res, err = in.c.ListPodSandboxMetrics(ctx, r)
	return res, errgrpc.ToGRPC(err)
}

func (in *instrumentedService) RuntimeConfig(ctx context.Context, r *runtime.RuntimeConfigRequest) (res *runtime.RuntimeConfigResponse, err error) {
	if err := in.checkInitialized(); err != nil {
		return nil, err
	}
	log.G(ctx).Tracef("RuntimeConfig")
	defer func() {
		if err != nil {
			log.G(ctx).WithError(err).Error("RuntimeConfig failed")
		} else {
			log.G(ctx).Tracef("RuntimeConfig returns config %+v", res)
		}
	}()
	res, err = in.c.RuntimeConfig(ctx, r)
	return res, errgrpc.ToGRPC(err)
}

func (in *instrumentedService) UpdatePodSandboxResources(ctx context.Context, r *runtime.UpdatePodSandboxResourcesRequest) (*runtime.UpdatePodSandboxResourcesResponse, error) {
	return call(ctx, in, r, in.c.UpdatePodSandboxResources, untraced,
		infof("UpdatePodSandboxResources for %q with Overhead: %+v / Resources: %+v", r.GetPodSandboxId(), r.GetOverhead(), r.GetResources()),
		errorf("UpdatePodSandboxResources for %q failed", r.GetPodSandboxId()),
		func(*runtime.UpdatePodSandboxResourcesResponse) logLine {
			return infof("UpdatePodSandboxResources for %q returns successfully", r.GetPodSandboxId())
		})
}

// withNamespaceContext wraps a server streaming server to inject the
// containerd namespace into its context, matching the unary method pattern.
func withNamespaceContext[Res any](s grpc.ServerStreamingServer[Res]) grpc.ServerStreamingServer[Res] {
	return &wrappedServerStreamingServer[Res]{
		ServerStreamingServer: s,
		ctx:                   ctrdutil.WithNamespace(s.Context()),
	}
}

type wrappedServerStreamingServer[Res any] struct {
	grpc.ServerStreamingServer[Res]
	ctx context.Context
}

func (w *wrappedServerStreamingServer[Res]) Context() context.Context {
	return w.ctx
}

func (in *instrumentedService) StreamContainers(r *runtime.StreamContainersRequest, s grpc.ServerStreamingServer[runtime.StreamContainersResponse]) (err error) {
	if err := in.checkInitialized(); err != nil {
		return err
	}
	ctx := s.Context()
	log.G(ctx).Tracef("StreamContainers with filter %+v", r.GetFilter())
	defer func() {
		if err != nil {
			log.G(ctx).WithError(err).Errorf("StreamContainers with filter %+v failed", r.GetFilter())
		} else {
			log.G(ctx).Tracef("StreamContainers returns successfully")
		}
	}()
	err = in.c.StreamContainers(r, withNamespaceContext(s))
	return errgrpc.ToGRPC(err)
}

func (in *instrumentedService) StreamPodSandboxes(r *runtime.StreamPodSandboxesRequest, s grpc.ServerStreamingServer[runtime.StreamPodSandboxesResponse]) (err error) {
	if err := in.checkInitialized(); err != nil {
		return err
	}
	ctx := s.Context()
	log.G(ctx).Tracef("StreamPodSandboxes with filter %+v", r.GetFilter())
	defer func() {
		if err != nil {
			log.G(ctx).WithError(err).Errorf("StreamPodSandboxes with filter %+v failed", r.GetFilter())
		} else {
			log.G(ctx).Tracef("StreamPodSandboxes returns successfully")
		}
	}()
	err = in.c.StreamPodSandboxes(r, withNamespaceContext(s))
	return errgrpc.ToGRPC(err)
}

func (in *instrumentedService) StreamContainerStats(r *runtime.StreamContainerStatsRequest, s grpc.ServerStreamingServer[runtime.StreamContainerStatsResponse]) (err error) {
	if err := in.checkInitialized(); err != nil {
		return err
	}
	ctx := s.Context()
	log.G(ctx).Tracef("StreamContainerStats with filter %+v", r.GetFilter())
	defer func() {
		if err != nil {
			log.G(ctx).WithError(err).Errorf("StreamContainerStats with filter %+v failed", r.GetFilter())
		} else {
			log.G(ctx).Tracef("StreamContainerStats returns successfully")
		}
	}()
	err = in.c.StreamContainerStats(r, withNamespaceContext(s))
	return errgrpc.ToGRPC(err)
}

func (in *instrumentedService) StreamPodSandboxStats(r *runtime.StreamPodSandboxStatsRequest, s grpc.ServerStreamingServer[runtime.StreamPodSandboxStatsResponse]) (err error) {
	if err := in.checkInitialized(); err != nil {
		return err
	}
	ctx := s.Context()
	log.G(ctx).Tracef("StreamPodSandboxStats with filter %+v", r.GetFilter())
	defer func() {
		if err != nil {
			log.G(ctx).WithError(err).Errorf("StreamPodSandboxStats with filter %+v failed", r.GetFilter())
		} else {
			log.G(ctx).Tracef("StreamPodSandboxStats returns successfully")
		}
	}()
	err = in.c.StreamPodSandboxStats(r, withNamespaceContext(s))
	return errgrpc.ToGRPC(err)
}

func (in *instrumentedService) StreamPodSandboxMetrics(r *runtime.StreamPodSandboxMetricsRequest, s grpc.ServerStreamingServer[runtime.StreamPodSandboxMetricsResponse]) (err error) {
	if err := in.checkInitialized(); err != nil {
		return err
	}
	ctx := s.Context()
	log.G(ctx).Tracef("StreamPodSandboxMetrics")
	defer func() {
		if err != nil {
			log.G(ctx).WithError(err).Errorf("StreamPodSandboxMetrics failed")
		} else {
			log.G(ctx).Tracef("StreamPodSandboxMetrics returns successfully")
		}
	}()
	err = in.c.StreamPodSandboxMetrics(r, withNamespaceContext(s))
	return errgrpc.ToGRPC(err)
}

func (in *instrumentedService) StreamImages(r *runtime.StreamImagesRequest, s grpc.ServerStreamingServer[runtime.StreamImagesResponse]) (err error) {
	if err := in.checkInitialized(); err != nil {
		return err
	}
	ctx := s.Context()
	log.G(ctx).Tracef("StreamImages with filter %+v", r.GetFilter())
	defer func() {
		if err != nil {
			err = ctrdutil.SanitizeError(err)
			log.G(ctx).WithError(err).Errorf("StreamImages with filter %+v failed", r.GetFilter())
		} else {
			log.G(ctx).Tracef("StreamImages returns successfully")
		}
	}()
	err = in.c.StreamImages(r, withNamespaceContext(s))
	if err != nil {
		err = ctrdutil.SanitizeError(err)
	}
	return errgrpc.ToGRPC(err)
}
