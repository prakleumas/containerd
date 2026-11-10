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

package walking

import (
	"context"
	"io"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/containerd/containerd/v2/core/content"
	"github.com/containerd/containerd/v2/core/diff"
	"github.com/containerd/containerd/v2/core/mount"
	"github.com/containerd/containerd/v2/pkg/archive"
	"github.com/containerd/containerd/v2/plugins/diff/internal/layerwriter"
)

type walkingDiff struct {
	store content.Store
}

var emptyDesc = ocispec.Descriptor{}

// NewWalkingDiff is a generic implementation of diff.Comparer.  The diff is
// calculated by mounting both the upper and lower mount sets and walking the
// mounted directories concurrently. Changes are calculated by comparing files
// against each other or by comparing file existence between directories.
// NewWalkingDiff uses no special characteristics of the mount sets and is
// expected to work with any filesystem.
func NewWalkingDiff(store content.Store) diff.Comparer {
	return &walkingDiff{
		store: store,
	}
}

// Compare creates a diff between the given mounts and uploads the result
// to the content store.
func (s *walkingDiff) Compare(ctx context.Context, lower, upper []mount.Mount, opts ...diff.Opt) (d ocispec.Descriptor, err error) {
	config, err := layerwriter.Prepare(ctx, opts...)
	if err != nil {
		return emptyDesc, err
	}

	var writeDiffOpts []archive.WriteDiffOpt
	if config.SourceDateEpoch != nil {
		writeDiffOpts = append(writeDiffOpts, archive.WithSourceDateEpoch(config.SourceDateEpoch))
	}

	var ocidesc ocispec.Descriptor
	if err := mount.WithTempMount(ctx, lower, func(lowerRoot string) error {
		return mount.WithReadonlyTempMount(ctx, upper, func(upperRoot string) error {
			var err error
			ocidesc, err = layerwriter.Write(ctx, s.store, config, func(w io.Writer) error {
				return archive.WriteDiff(ctx, w, lowerRoot, upperRoot, writeDiffOpts...)
			})
			return err
		})
	}); err != nil {
		return emptyDesc, err
	}

	return ocidesc, nil
}
