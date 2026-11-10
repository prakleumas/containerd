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

// Package diffapply reads layer content through the stream processors
// shared by the diff appliers.
package diffapply

import (
	"context"
	"fmt"
	"io"

	"github.com/containerd/containerd/v2/core/content"
	"github.com/containerd/containerd/v2/core/diff"
	digest "github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// Apply reads the content of desc from cs, runs it through the stream
// processors that turn mediaType into an uncompressed tar stream, and passes
// that stream to apply. Data that apply leaves unread is read and discarded.
// It returns the digest and size of the uncompressed stream.
func Apply(ctx context.Context, cs content.Provider, desc ocispec.Descriptor, mediaType string, config diff.ApplyConfig, apply func(io.Reader) error) (digest.Digest, int64, error) {
	ra, err := cs.ReaderAt(ctx, desc)
	if err != nil {
		return "", 0, fmt.Errorf("failed to get reader from content store: %w", err)
	}
	var r io.ReadCloser
	if config.Progress != nil {
		r = newProgressReader(ra, config.Progress)
	} else {
		r = newReadCloser(ra)
	}
	defer r.Close()

	var processors []diff.StreamProcessor
	processor := diff.NewProcessorChain(mediaType, r)
	processors = append(processors, processor)
	for {
		if processor, err = diff.GetProcessor(ctx, processor, config.ProcessorPayloads); err != nil {
			return "", 0, fmt.Errorf("failed to get stream processor for %s: %w", desc.MediaType, err)
		}
		processors = append(processors, processor)
		if processor.MediaType() == ocispec.MediaTypeImageLayer {
			break
		}
	}
	defer processor.Close()

	digester := digest.Canonical.Digester()
	rc := &readCounter{
		r: io.TeeReader(processor, digester.Hash()),
	}

	if err := apply(rc); err != nil {
		return "", 0, err
	}

	// Read any trailing data
	if _, err := io.Copy(io.Discard, rc); err != nil {
		return "", 0, err
	}

	for _, p := range processors {
		if ep, ok := p.(interface {
			Err() error
		}); ok {
			if err := ep.Err(); err != nil {
				return "", 0, err
			}
		}
	}

	return digester.Digest(), rc.c, nil
}

type readCounter struct {
	r io.Reader
	c int64
}

func (rc *readCounter) Read(p []byte) (n int, err error) {
	n, err = rc.r.Read(p)
	if n > 0 {
		rc.c += int64(n)
	}
	return
}

type progressReader struct {
	rc *readCounter
	c  io.Closer
	p  func(int64)
}

func newProgressReader(ra content.ReaderAt, p func(int64)) io.ReadCloser {
	return &progressReader{
		rc: &readCounter{
			r: content.NewReader(ra),
			c: 0,
		},
		c: ra,
		p: p,
	}
}

func (pr *progressReader) Read(p []byte) (n int, err error) {
	// Call the progress function with the current count, indicating
	// the previously read content has been processed. Initial
	// progress of 0 indicates start of processing.
	pr.p(pr.rc.c)
	n, err = pr.rc.Read(p)
	return
}

func (pr *progressReader) Close() error {
	pr.p(pr.rc.c)
	return pr.c.Close()
}

type readCloser struct {
	io.Reader
	io.Closer
}

func newReadCloser(ra content.ReaderAt) io.ReadCloser {
	return &readCloser{
		Reader: content.NewReader(ra),
		Closer: ra,
	}
}
