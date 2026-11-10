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
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	runtime "k8s.io/cri-api/pkg/apis/runtime/v1"

	sandboxstore "github.com/containerd/containerd/v2/internal/cri/store/sandbox"
)

func TestStreamPodSandboxesEmpty(t *testing.T) {
	c := newTestCRIService()

	stream := newFakeStreamServer[runtime.StreamPodSandboxesResponse](context.Background())
	err := c.StreamPodSandboxes(&runtime.StreamPodSandboxesRequest{}, stream)
	assert.NoError(t, err)
	assert.Empty(t, stream.sent, "no messages should be sent for empty store")
}

func TestStreamPodSandboxesBatching(t *testing.T) {
	c := newTestCRIService()

	// Add more sandboxes than the batch size to verify batching
	for i := 0; i < 3; i++ {
		sb := sandboxstore.NewSandbox(
			sandboxstore.Metadata{
				ID:   fmt.Sprintf("sb-%d", i),
				Name: fmt.Sprintf("sandbox-%d", i),
				Config: &runtime.PodSandboxConfig{
					Metadata: &runtime.PodSandboxMetadata{
						Name: fmt.Sprintf("pod-%d", i),
					},
				},
			},
			sandboxstore.Status{
				CreatedAt: time.Now(),
				State:     sandboxstore.StateReady,
			},
		)
		require.NoError(t, c.sandboxStore.Add(sb))
	}

	stream := newFakeStreamServer[runtime.StreamPodSandboxesResponse](context.Background())
	err := c.StreamPodSandboxes(&runtime.StreamPodSandboxesRequest{}, stream)
	assert.NoError(t, err)

	// With 3 sandboxes and batch size of 5000, should be 1 batch
	require.Len(t, stream.sent, 1)
	assert.Len(t, stream.sent[0].PodSandboxes, 3)
}
