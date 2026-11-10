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
	"os"
	"path/filepath"

	"github.com/containerd/containerd/v2/core/snapshots/storage"
)

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
