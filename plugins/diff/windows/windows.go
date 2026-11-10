//go:build windows

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

package windows

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/Microsoft/go-winio"
	"github.com/containerd/errdefs"
	"github.com/containerd/log"
	"github.com/containerd/platforms"
	"github.com/containerd/plugin"
	"github.com/containerd/plugin/registry"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/containerd/containerd/v2/core/content"
	"github.com/containerd/containerd/v2/core/diff"
	"github.com/containerd/containerd/v2/core/metadata"
	"github.com/containerd/containerd/v2/core/mount"
	"github.com/containerd/containerd/v2/pkg/archive"
	"github.com/containerd/containerd/v2/plugins"
	"github.com/containerd/containerd/v2/plugins/diff/internal/layerwriter"
)

func init() {
	registry.Register(&plugin.Registration{
		Type: plugins.DiffPlugin,
		ID:   "windows",
		Requires: []plugin.Type{
			plugins.MetadataPlugin,
		},
		InitFn: func(ic *plugin.InitContext) (any, error) {
			md, err := ic.GetSingle(plugins.MetadataPlugin)
			if err != nil {
				return nil, err
			}

			ic.Meta.Platforms = append(ic.Meta.Platforms, platforms.DefaultSpec())
			return NewWindowsDiff(md.(*metadata.DB).ContentStore())
		},
	})
}

// CompareApplier handles both comparison and
// application of layer diffs.
type CompareApplier interface {
	diff.Applier
	diff.Comparer
}

// windowsDiff does filesystem comparison and application
// for Windows specific layer diffs.
type windowsDiff struct {
	store content.Store
}

var emptyDesc = ocispec.Descriptor{}

// NewWindowsDiff is the Windows container layer implementation
// for comparing and applying filesystem layers
func NewWindowsDiff(store content.Store) (CompareApplier, error) {
	return windowsDiff{
		store: store,
	}, nil
}

// Apply applies the content associated with the provided digests onto the
// provided mounts. Archive content will be extracted and decompressed if
// necessary.
func (s windowsDiff) Apply(ctx context.Context, desc ocispec.Descriptor, mounts []mount.Mount, opts ...diff.ApplyOpt) (d ocispec.Descriptor, err error) {
	layerPath, parentLayerPaths, err := mountsToLayerAndParents(mounts)
	if err != nil {
		return emptyDesc, err
	}

	// TODO darrenstahlmsft: When this is done isolated, we should disable these.
	// it currently cannot be disabled, unless we add ref counting. Since this is
	// temporary, leaving it enabled is OK for now.
	// https://github.com/containerd/containerd/issues/1681
	if err := winio.EnableProcessPrivileges([]string{winio.SeBackupPrivilege, winio.SeRestorePrivilege}); err != nil {
		return emptyDesc, err
	}

	t1 := time.Now()
	defer func() {
		if err == nil {
			log.G(ctx).WithFields(log.Fields{
				"d":      time.Since(t1),
				"digest": desc.Digest,
				"size":   desc.Size,
				"media":  desc.MediaType,
			}).Debug("diff applied")
		}
	}()

	var config diff.ApplyConfig
	for _, o := range opts {
		if err := o(ctx, desc, &config); err != nil {
			return emptyDesc, fmt.Errorf("failed to apply config opt: %w", err)
		}
	}

	ra, err := s.store.ReaderAt(ctx, desc)
	if err != nil {
		return emptyDesc, fmt.Errorf("failed to get reader from content store: %w", err)
	}
	defer ra.Close()

	processor := diff.NewProcessorChain(desc.MediaType, content.NewReader(ra))
	for {
		if processor, err = diff.GetProcessor(ctx, processor, config.ProcessorPayloads); err != nil {
			return emptyDesc, fmt.Errorf("failed to get stream processor for %s: %w", desc.MediaType, err)
		}
		if processor.MediaType() == ocispec.MediaTypeImageLayer {
			break
		}
	}
	defer processor.Close()

	digester := digest.Canonical.Digester()
	rc := &readCounter{
		r: io.TeeReader(processor, digester.Hash()),
	}

	archiveOpts := []archive.ApplyOpt{
		archive.WithParents(parentLayerPaths),
		archive.WithNoSameOwner(), // Lchown is not supported on Windows
		archive.AsWindowsContainerLayer(),
	}

	if _, err := archive.Apply(ctx, layerPath, rc, archiveOpts...); err != nil {
		return emptyDesc, err
	}

	// Read any trailing data
	if _, err := io.Copy(io.Discard, rc); err != nil {
		return emptyDesc, err
	}

	return ocispec.Descriptor{
		MediaType: ocispec.MediaTypeImageLayer,
		Size:      rc.c,
		Digest:    digester.Digest(),
	}, nil

}

// Compare creates a diff between the given mounts and uploads the result
// to the content store.
func (s windowsDiff) Compare(ctx context.Context, lower, upper []mount.Mount, opts ...diff.Opt) (d ocispec.Descriptor, err error) {
	t1 := time.Now()

	layers, err := mountPairToLayerStack(lower, upper)
	if err != nil {
		return emptyDesc, err
	}

	config, err := layerwriter.Prepare(ctx, opts...)
	if err != nil {
		return emptyDesc, err
	}

	// TODO darrenstahlmsft: When this is done isolated, we should disable this.
	// it currently cannot be disabled, unless we add ref counting. Since this is
	// temporary, leaving it enabled is OK for now.
	// https://github.com/containerd/containerd/issues/1681
	if err := winio.EnableProcessPrivileges([]string{winio.SeBackupPrivilege}); err != nil {
		return emptyDesc, err
	}

	desc, err := layerwriter.Write(ctx, s.store, config, func(w io.Writer) error {
		return archive.WriteDiff(ctx, w, "", layers[0], archive.AsWindowsContainerLayerPair(), archive.WithParentLayers(layers[1:]))
	})
	if err != nil {
		return emptyDesc, err
	}

	log.G(ctx).WithFields(log.Fields{
		"d":     time.Since(t1),
		"dgst":  desc.Digest,
		"size":  desc.Size,
		"media": desc.MediaType,
	}).Debug("diff created")

	return desc, nil
}

type readCounter struct {
	r io.Reader
	c int64
}

func (rc *readCounter) Read(p []byte) (n int, err error) {
	n, err = rc.r.Read(p)
	rc.c += int64(n)
	return
}

func mountsToLayerAndParents(mounts []mount.Mount) (string, []string, error) {
	if len(mounts) != 1 {
		return "", nil, fmt.Errorf("number of mounts should always be 1 for Windows layers: %w", errdefs.ErrInvalidArgument)
	}
	mnt := mounts[0]

	if mnt.Type != "windows-layer" {
		// This is a special case error. When this is received the diff service
		// will attempt the next differ in the chain which for Windows is the
		// lcow differ that we want.
		return "", nil, fmt.Errorf("windowsDiff does not support layer type %s: %w", mnt.Type, errdefs.ErrNotImplemented)
	}

	parentLayerPaths, err := mnt.GetParentPaths()
	if err != nil {
		return "", nil, err
	}

	if mnt.ReadOnly() {
		if len(parentLayerPaths) == 0 {
			// rootfs.CreateDiff creates a new, empty View to diff against,
			// when diffing something with no parent.
			// This makes perfect sense for a walking Diff, but for WCOW,
			// we have to recognise this as "diff against nothing"
			return "", nil, nil
		}
		// Ignore the dummy sandbox.
		return parentLayerPaths[0], parentLayerPaths[1:], nil
	}
	return mnt.Source, parentLayerPaths, nil
}

// mountPairToLayerStack ensures that the two sets of mount-lists are actually a correct
// parent-and-child, or orphan-and-empty-list, and return the full list of layers, starting
// with the upper-most (most childish?)
func mountPairToLayerStack(lower, upper []mount.Mount) ([]string, error) {

	// May return an ErrNotImplemented, which will fall back to LCOW
	upperLayer, upperParentLayerPaths, err := mountsToLayerAndParents(upper)
	if err != nil {
		return nil, fmt.Errorf("upper mount invalid: %w", err)
	}

	lowerLayer, lowerParentLayerPaths, err := mountsToLayerAndParents(lower)
	if errdefs.IsNotImplemented(err) {
		// Upper was a windows-layer, lower is not. We can't handle that.
		return nil, fmt.Errorf("windowsDiff cannot diff a windows-layer against a non-windows-layer: %w", errdefs.ErrInvalidArgument)
	} else if err != nil {
		return nil, fmt.Errorf("lower mount invalid: %w", err)
	}

	// Trivial case, diff-against-nothing
	if lowerLayer == "" {
		if len(upperParentLayerPaths) != 0 {
			return nil, fmt.Errorf("windowsDiff cannot diff a layer with parents against a null layer: %w", errdefs.ErrInvalidArgument)
		}
		return []string{upperLayer}, nil
	}

	if len(upperParentLayerPaths) < 1 {
		return nil, fmt.Errorf("windowsDiff cannot diff a layer with no parents against another layer: %w", errdefs.ErrInvalidArgument)
	}

	if upperParentLayerPaths[0] != lowerLayer {
		return nil, fmt.Errorf("windowsDiff cannot diff a layer against a layer other than its own parent: %w", errdefs.ErrInvalidArgument)
	}

	if len(upperParentLayerPaths) != len(lowerParentLayerPaths)+1 {
		return nil, fmt.Errorf("windowsDiff cannot diff a layer against a layer with different parents: %w", errdefs.ErrInvalidArgument)
	}
	for i, upperParent := range upperParentLayerPaths[1:] {
		if upperParent != lowerParentLayerPaths[i] {
			return nil, fmt.Errorf("windowsDiff cannot diff a layer against a layer with different parents: %w", errdefs.ErrInvalidArgument)
		}
	}

	return append([]string{upperLayer}, upperParentLayerPaths...), nil
}
