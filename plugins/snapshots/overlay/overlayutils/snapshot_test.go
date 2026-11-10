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
	"testing"

	"github.com/containerd/containerd/v2/core/snapshots"
)

func TestIDMapRoot(t *testing.T) {
	for _, tc := range []struct {
		name     string
		labels   map[string]string
		uid, gid int
		wantErr  bool
	}{
		{
			name:   "no mapping labels",
			labels: map[string]string{"other": "value"},
			uid:    -1,
			gid:    -1,
		},
		{
			name: "uid and gid mappings",
			labels: map[string]string{
				snapshots.LabelSnapshotUIDMapping: "0:1000:65536",
				snapshots.LabelSnapshotGIDMapping: "0:2000:65536",
			},
			uid: 1000,
			gid: 2000,
		},
		{
			name: "uid mapping only",
			labels: map[string]string{
				snapshots.LabelSnapshotUIDMapping: "0:1000:65536",
			},
			uid: 1000,
			gid: 0,
		},
		{
			name: "malformed mapping",
			labels: map[string]string{
				snapshots.LabelSnapshotUIDMapping: "0:1000",
			},
			wantErr: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			uid, gid, err := IDMapRoot(tc.labels)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("IDMapRoot(%v) = %d, %d, nil; want error", tc.labels, uid, gid)
				}
				return
			}
			if err != nil {
				t.Fatalf("IDMapRoot(%v) error: %v", tc.labels, err)
			}
			if uid != tc.uid || gid != tc.gid {
				t.Errorf("IDMapRoot(%v) = %d, %d; want %d, %d", tc.labels, uid, gid, tc.uid, tc.gid)
			}
		})
	}
}
