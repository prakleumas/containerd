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

// Package metastore provides snapshotter methods shared by snapshotters
// that keep their metadata in a storage.MetaStore.
package metastore

import (
	"context"

	"github.com/containerd/containerd/v2/core/snapshots"
	"github.com/containerd/containerd/v2/core/snapshots/storage"
)

// Base implements the Stat, Update, Walk and Close methods of
// snapshots.Snapshotter on a storage.MetaStore. A snapshotter embeds it
// to get those methods.
type Base struct {
	ms *storage.MetaStore
}

// NewBase returns a Base backed by ms.
func NewBase(ms *storage.MetaStore) Base {
	return Base{ms: ms}
}

// Stat returns the info for an active or committed snapshot by name or
// key.
//
// Should be used for parent resolution, existence checks and to discern
// the kind of snapshot.
func (b Base) Stat(ctx context.Context, key string) (info snapshots.Info, err error) {
	err = b.ms.WithTransaction(ctx, false, func(ctx context.Context) error {
		_, info, _, err = storage.GetInfo(ctx, key)
		return err
	})
	if err != nil {
		return snapshots.Info{}, err
	}

	return info, nil
}

// Update updates the info for a snapshot.
func (b Base) Update(ctx context.Context, info snapshots.Info, fieldpaths ...string) (_ snapshots.Info, err error) {
	err = b.ms.WithTransaction(ctx, true, func(ctx context.Context) error {
		info, err = storage.UpdateInfo(ctx, info, fieldpaths...)
		return err
	})
	if err != nil {
		return snapshots.Info{}, err
	}

	return info, nil
}

// Walk the committed snapshots.
func (b Base) Walk(ctx context.Context, fn snapshots.WalkFunc, fs ...string) error {
	return b.ms.WithTransaction(ctx, false, func(ctx context.Context) error {
		return storage.WalkInfo(ctx, fn, fs...)
	})
}

// Close closes the snapshotter.
func (b Base) Close() error {
	return b.ms.Close()
}
