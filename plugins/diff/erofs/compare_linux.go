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

package erofs

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	"github.com/containerd/continuity/fs"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/containerd/containerd/v2/core/diff"
	"github.com/containerd/containerd/v2/core/mount"
	"github.com/containerd/containerd/v2/internal/erofsutils"
	"github.com/containerd/containerd/v2/pkg/archive"
	"github.com/containerd/containerd/v2/plugins/diff/internal/layerwriter"
)

func writeDiff(ctx context.Context, w io.Writer, lower []mount.Mount, upperRoot string) error {
	var opts []archive.ChangeWriterOpt

	return mount.WithTempMount(ctx, lower, func(lowerRoot string) error {
		cw := archive.NewChangeWriter(w, upperRoot, opts...)
		err := fs.DiffDirChanges(ctx, lowerRoot, upperRoot, fs.DiffSourceOverlayFS, cw.HandleChange)
		if err != nil {
			return fmt.Errorf("failed to create diff tar stream: %w", err)
		}
		return cw.Close()
	})
}

// Compare creates a diff between the given mounts and uploads the result
// to the content store.
func (s erofsDiff) Compare(ctx context.Context, lower, upper []mount.Mount, opts ...diff.Opt) (d ocispec.Descriptor, err error) {
	layer, err := erofsutils.MountsToLayer(upper)
	if err != nil {
		return emptyDesc, fmt.Errorf("unsupported layer for erofsDiff Compare method: %w", err)
	}

	config, err := layerwriter.Prepare(ctx, opts...)
	if err != nil {
		return emptyDesc, err
	}

	upperRoot := filepath.Join(layer, "fs")
	return layerwriter.Write(ctx, s.store, config, func(w io.Writer) error {
		return writeDiff(ctx, w, lower, upperRoot)
	})
}
