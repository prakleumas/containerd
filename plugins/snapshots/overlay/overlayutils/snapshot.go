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

package overlayutils

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/containerd/containerd/v2/core/snapshots"
	"github.com/containerd/containerd/v2/core/snapshots/storage"
	"github.com/containerd/containerd/v2/internal/userns"
)

// IDMapRoot returns the host UID and GID that container root maps to under
// the snapshot ID mapping labels in labels. It returns -1, -1 when labels
// has neither mapping label.
func IDMapRoot(labels map[string]string) (uid, gid int, err error) {
	uidmapLabel, hasUID := labels[snapshots.LabelSnapshotUIDMapping]
	gidmapLabel, hasGID := labels[snapshots.LabelSnapshotGIDMapping]
	if !hasUID && !hasGID {
		return -1, -1, nil
	}

	var idMap userns.IDMap
	if err := idMap.Unmarshal(uidmapLabel, gidmapLabel); err != nil {
		return -1, -1, fmt.Errorf("failed to unmarshal snapshot ID mapped labels: %w", err)
	}
	root, err := idMap.RootPair()
	if err != nil {
		return -1, -1, fmt.Errorf("failed to find root pair: %w", err)
	}
	return int(root.Uid), int(root.Gid), nil
}

// OrphanDirs returns the paths of the entries in snapshotDir that have no
// snapshot ID in the metadata store, skipping each name for which skip
// returns true. skip may be nil. ctx must carry a metadata store
// transaction.
func OrphanDirs(ctx context.Context, snapshotDir string, skip func(name string) bool) ([]string, error) {
	ids, err := storage.IDMap(ctx)
	if err != nil {
		return nil, err
	}

	fd, err := os.Open(snapshotDir)
	if err != nil {
		return nil, err
	}
	defer fd.Close()

	dirs, err := fd.Readdirnames(0)
	if err != nil {
		return nil, err
	}

	cleanup := []string{}
	for _, d := range dirs {
		if skip != nil && skip(d) {
			continue
		}
		if _, ok := ids[d]; ok {
			continue
		}
		cleanup = append(cleanup, filepath.Join(snapshotDir, d))
	}

	return cleanup, nil
}
