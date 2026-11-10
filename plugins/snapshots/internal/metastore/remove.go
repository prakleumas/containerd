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

// Package metastore holds snapshotter helpers built on storage.MetaStore.
package metastore

import (
	"context"
	"fmt"
	"os"

	"github.com/containerd/containerd/v2/core/snapshots/storage"
	"github.com/containerd/log"
)

// Remove removes the snapshot key from ms and, in the same write
// transaction, renames the snapshot's path, pathOf(id), to pathOf("rm-"+id).
// If the transaction fails to commit, the rename is undone. Remove returns
// the renamed path, which the caller deletes, or "" when the snapshot's path
// did not exist.
func Remove(ctx context.Context, ms *storage.MetaStore, key string, pathOf func(id string) string) (renamed string, err error) {
	var path string
	err = ms.WithTransaction(ctx, true, func(ctx context.Context) error {
		id, _, err := storage.Remove(ctx, key)
		if err != nil {
			return fmt.Errorf("failed to remove: %w", err)
		}

		path = pathOf(id)
		rm := pathOf("rm-" + id)
		if err := os.Rename(path, rm); err != nil {
			if !os.IsNotExist(err) {
				return fmt.Errorf("failed to rename: %w", err)
			}
			return nil
		}
		renamed = rm
		return nil
	})
	if err != nil {
		if renamed != "" {
			if err1 := os.Rename(renamed, path); err1 != nil {
				// May cause inconsistent data on disk
				log.G(ctx).WithError(err1).WithField("path", renamed).Error("failed to rename after failed commit")
			}
		}
		return "", err
	}
	return renamed, nil
}
