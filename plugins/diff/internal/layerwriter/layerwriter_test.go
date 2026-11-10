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

package layerwriter

import (
	"context"
	"errors"
	"io"
	"testing"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/containerd/containerd/v2/core/content"
	"github.com/containerd/containerd/v2/core/diff"
	"github.com/containerd/containerd/v2/plugins/content/local"
)

// TestWriteFailureReleasesReference checks that a failed write closes the
// content writer, so the caller can open its reference again.
func TestWriteFailureReleasesReference(t *testing.T) {
	for _, mediaType := range []string{ocispec.MediaTypeImageLayer, ocispec.MediaTypeImageLayerGzip} {
		t.Run(mediaType, func(t *testing.T) {
			ctx := context.Background()
			store, err := local.NewStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			const ref = "caller-ref"
			config, err := Prepare(ctx, diff.WithReference(ref), diff.WithMediaType(mediaType))
			if err != nil {
				t.Fatal(err)
			}

			errWrite := errors.New("write failed")
			_, err = Write(ctx, store, config, func(io.Writer) error { return errWrite })
			if !errors.Is(err, errWrite) {
				t.Fatalf("Write() error = %v, want %v", err, errWrite)
			}

			cw, err := store.Writer(ctx, content.WithRef(ref))
			if err != nil {
				t.Fatalf("Writer(%q) after failed Write: %v", ref, err)
			}
			cw.Close()
		})
	}
}
