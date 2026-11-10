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
	"google.golang.org/grpc"
	runtime "k8s.io/cri-api/pkg/apis/runtime/v1"

	criutil "github.com/containerd/containerd/v2/internal/cri/util"
)

// StreamPodSandboxStats streams stats of all ready sandboxes matching the filter.
func (c *criService) StreamPodSandboxStats(r *runtime.StreamPodSandboxStatsRequest, s grpc.ServerStreamingServer[runtime.StreamPodSandboxStatsResponse]) error {
	ctx := s.Context()
	listReq := &runtime.ListPodSandboxStatsRequest{
		Filter: r.GetFilter(),
	}
	sandboxes := c.sandboxesForListPodSandboxStatsRequest(listReq)
	podSandboxStats, err := c.collectPodSandboxStats(ctx, sandboxes)
	if err != nil {
		return err
	}

	return criutil.SendInBatches(ctx, podSandboxStats, criutil.DefaultStreamBatchSize, func(batch []*runtime.PodSandboxStats) error {
		return s.Send(&runtime.StreamPodSandboxStatsResponse{PodSandboxStats: batch})
	})
}
