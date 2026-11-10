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

package streaming

import (
	"context"
	"testing"

	"github.com/containerd/containerd/v2/core/leases"
	"github.com/containerd/containerd/v2/core/metadata"
	"github.com/containerd/containerd/v2/pkg/gc"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/typeurl/v2"
)

type fakeStream struct {
	closed bool
}

func (s *fakeStream) Send(typeurl.Any) error     { return nil }
func (s *fakeStream) Recv() (typeurl.Any, error) { return nil, nil }
func (s *fakeStream) Close() error               { s.closed = true; return nil }

func TestStreamRemoval(t *testing.T) {
	sm := &streamManager{
		streams: map[string]map[string]*managedStream{},
		byLease: map[string]map[string]map[string]struct{}{},
	}
	ctx := leases.WithLease(namespaces.WithNamespace(context.Background(), "ns"), "lease")

	closed, collected := &fakeStream{}, &fakeStream{}
	if err := sm.Register(ctx, "closed", closed); err != nil {
		t.Fatalf("Register(closed) = %v", err)
	}
	if err := sm.Register(ctx, "collected", collected); err != nil {
		t.Fatalf("Register(collected) = %v", err)
	}

	s, err := sm.Get(ctx, "closed")
	if err != nil {
		t.Fatalf("Get(closed) = %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close() = %v", err)
	}
	if !closed.closed {
		t.Error("Close() did not close the underlying stream")
	}

	leased := func(cc metadata.CollectionContext) []string {
		var names []string
		cc.Leased("ns", "lease", func(n gc.Node) { names = append(names, n.Key) })
		return names
	}

	cc, err := sm.StartCollection(ctx)
	if err != nil {
		t.Fatalf("StartCollection() = %v", err)
	}
	if got := leased(cc); len(got) != 1 || got[0] != "collected" {
		t.Errorf("Leased after Close = %v, want [collected]", got)
	}
	cc.Remove(gc.Node{Type: metadata.ResourceStream, Namespace: "ns", Key: "collected"})
	if err := cc.Finish(); err != nil {
		t.Fatalf("Finish() = %v", err)
	}
	if !collected.closed {
		t.Error("Finish() did not close the removed stream")
	}

	if len(sm.streams) != 0 || len(sm.byLease) != 0 {
		t.Errorf("after removing every stream: streams = %v, byLease = %v, want both empty", sm.streams, sm.byLease)
	}
}
