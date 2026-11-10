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

package images

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/metadata"
	runtime "k8s.io/cri-api/pkg/apis/runtime/v1"
)

// fakeStreamServer is a mock grpc.ServerStreamingServer that captures sent messages.
type fakeStreamServer[Res any] struct {
	ctx  context.Context
	sent []*Res
}

func newFakeStreamServer[Res any](ctx context.Context) *fakeStreamServer[Res] {
	return &fakeStreamServer[Res]{ctx: ctx}
}

func (f *fakeStreamServer[Res]) Send(msg *Res) error {
	f.sent = append(f.sent, msg)
	return nil
}

func (f *fakeStreamServer[Res]) SetHeader(metadata.MD) error  { return nil }
func (f *fakeStreamServer[Res]) SendHeader(metadata.MD) error { return nil }
func (f *fakeStreamServer[Res]) SetTrailer(metadata.MD)       {}
func (f *fakeStreamServer[Res]) Context() context.Context     { return f.ctx }
func (f *fakeStreamServer[Res]) SendMsg(any) error            { return nil }
func (f *fakeStreamServer[Res]) RecvMsg(any) error            { return nil }

func TestStreamImagesEmpty(t *testing.T) {
	_, c := newTestCRIService()

	stream := newFakeStreamServer[runtime.StreamImagesResponse](context.Background())
	err := c.StreamImages(&runtime.StreamImagesRequest{}, stream)
	assert.NoError(t, err)
	assert.Empty(t, stream.sent, "no messages should be sent for empty store")
}
