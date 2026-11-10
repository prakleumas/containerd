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

package diffapply

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/containerd/containerd/v2/core/content"
	"github.com/containerd/containerd/v2/core/diff"
	"github.com/containerd/typeurl/v2"
	digest "github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

type blobProvider []byte

func (b blobProvider) ReaderAt(context.Context, ocispec.Descriptor) (content.ReaderAt, error) {
	return blobReaderAt{bytes.NewReader(b)}, nil
}

type blobReaderAt struct {
	*bytes.Reader
}

func (blobReaderAt) Close() error { return nil }

func TestApplyCountsUnreadData(t *testing.T) {
	blob := []byte("layer data the apply function does not read")
	desc := ocispec.Descriptor{MediaType: ocispec.MediaTypeImageLayer}

	dgst, size, err := Apply(context.Background(), blobProvider(blob), desc, desc.MediaType, diff.ApplyConfig{}, func(r io.Reader) error {
		_, err := io.ReadFull(r, make([]byte, 5))
		return err
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if want := digest.FromBytes(blob); dgst != want {
		t.Errorf("Apply digest = %s, want %s", dgst, want)
	}
	if size != int64(len(blob)) {
		t.Errorf("Apply size = %d, want %d", size, len(blob))
	}
}

const failingMediaType = "application/vnd.test.failing-processor"

var errProcessor = errors.New("processor failed")

// failingProcessor passes its input through and reports a failure only
// through Err, as a stream processor run as an external binary does when
// the binary exits with an error.
type failingProcessor struct {
	diff.StreamProcessor
}

func (failingProcessor) MediaType() string { return ocispec.MediaTypeImageLayer }
func (failingProcessor) Err() error        { return errProcessor }

func init() {
	diff.RegisterProcessor(diff.StaticHandler(failingMediaType, func(_ context.Context, stream diff.StreamProcessor, _ map[string]typeurl.Any) (diff.StreamProcessor, error) {
		return failingProcessor{stream}, nil
	}))
}

func TestApplyReturnsProcessorErr(t *testing.T) {
	desc := ocispec.Descriptor{MediaType: failingMediaType}

	_, _, err := Apply(context.Background(), blobProvider("layer"), desc, desc.MediaType, diff.ApplyConfig{}, func(r io.Reader) error {
		_, err := io.Copy(io.Discard, r)
		return err
	})
	if !errors.Is(err, errProcessor) {
		t.Errorf("Apply error = %v, want %v", err, errProcessor)
	}
}
