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

// Package layerwriter writes a layer diff produced by a differ's Compare
// method to the content store.
package layerwriter

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/containerd/errdefs"
	"github.com/containerd/log"
	digest "github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/containerd/containerd/v2/core/content"
	"github.com/containerd/containerd/v2/core/diff"
	"github.com/containerd/containerd/v2/pkg/archive/compression"
	"github.com/containerd/containerd/v2/pkg/epoch"
	"github.com/containerd/containerd/v2/pkg/labels"
)

// Config is a diff.Config with its media type resolved to a compression.
type Config struct {
	diff.Config
	compression compression.Compression
}

// Prepare applies opts, defaults the source date epoch from ctx, and
// resolves the media type. Without a custom compressor the media type
// defaults to gzip and must be one of the OCI layer media types; with a
// custom compressor it must be set.
func Prepare(ctx context.Context, opts ...diff.Opt) (Config, error) {
	var config Config
	for _, opt := range opts {
		if err := opt(&config.Config); err != nil {
			return Config{}, err
		}
	}
	if tm := epoch.FromContext(ctx); tm != nil && config.SourceDateEpoch == nil {
		config.SourceDateEpoch = tm
	}

	config.compression = compression.Uncompressed
	if config.Compressor != nil {
		if config.MediaType == "" {
			return Config{}, errors.New("media type must be explicitly specified when using custom compressor")
		}
		config.compression = compression.Unknown
		return config, nil
	}
	if config.MediaType == "" {
		config.MediaType = ocispec.MediaTypeImageLayerGzip
	}
	switch config.MediaType {
	case ocispec.MediaTypeImageLayer:
	case ocispec.MediaTypeImageLayerGzip:
		config.compression = compression.Gzip
	case ocispec.MediaTypeImageLayerZstd:
		config.compression = compression.Zstd
	default:
		return Config{}, fmt.Errorf("unsupported diff media type: %v: %w", config.MediaType, errdefs.ErrNotImplemented)
	}
	return config, nil
}

// Write opens a content writer for config.Reference, or for a generated
// reference when it is empty, calls write to produce the uncompressed
// diff, and commits the compressed result with the
// "containerd.io/uncompressed" label. When anything before the commit
// fails, Write closes the writer and aborts a generated reference.
func Write(ctx context.Context, store content.Store, config Config, write func(io.Writer) error) (ocispec.Descriptor, error) {
	newReference := config.Reference == ""
	if newReference {
		config.Reference = uniqueRef()
	}

	cw, err := store.Writer(ctx,
		content.WithRef(config.Reference),
		content.WithDescriptor(ocispec.Descriptor{
			MediaType: config.MediaType, // most contentstore implementations just ignore this
		}))
	if err != nil {
		return ocispec.Descriptor{}, fmt.Errorf("failed to open writer: %w", err)
	}

	dgst, err := commit(ctx, cw, &config, !newReference, write)
	if err != nil {
		cw.Close()
		if newReference {
			if abortErr := store.Abort(ctx, config.Reference); abortErr != nil {
				log.G(ctx).WithError(abortErr).WithField("ref", config.Reference).Warnf("failed to delete diff upload")
			}
		}
		return ocispec.Descriptor{}, err
	}

	info, err := store.Info(ctx, dgst)
	if err != nil {
		return ocispec.Descriptor{}, fmt.Errorf("failed to get info from content store: %w", err)
	}
	if info.Labels == nil {
		info.Labels = make(map[string]string)
	}
	// Set "containerd.io/uncompressed" label if digest already existed without label
	if _, ok := info.Labels[labels.LabelUncompressed]; !ok {
		info.Labels[labels.LabelUncompressed] = config.Labels[labels.LabelUncompressed]
		if _, err := store.Update(ctx, info, "labels."+labels.LabelUncompressed); err != nil {
			return ocispec.Descriptor{}, fmt.Errorf("error setting uncompressed label: %w", err)
		}
	}

	return ocispec.Descriptor{
		MediaType: config.MediaType,
		Size:      info.Size,
		Digest:    info.Digest,
	}, nil
}

// commit writes the diff to cw and commits it, tolerating content that
// already exists. It sets the uncompressed digest label in config.Labels
// for a compressed diff.
func commit(ctx context.Context, cw content.Writer, config *Config, truncate bool, write func(io.Writer) error) (digest.Digest, error) {
	if truncate {
		if err := cw.Truncate(0); err != nil {
			return "", err
		}
	}

	if config.compression != compression.Uncompressed {
		dgstr := digest.SHA256.Digester()
		var compressed io.WriteCloser
		var err error
		if config.Compressor != nil {
			compressed, err = config.Compressor(cw, config.MediaType)
		} else {
			compressed, err = compression.CompressStream(cw, config.compression)
		}
		if err != nil {
			return "", fmt.Errorf("failed to get compressed stream: %w", err)
		}
		err = write(io.MultiWriter(compressed, dgstr.Hash()))
		compressed.Close()
		if err != nil {
			return "", fmt.Errorf("failed to write compressed diff: %w", err)
		}

		if config.Labels == nil {
			config.Labels = map[string]string{}
		}
		config.Labels[labels.LabelUncompressed] = dgstr.Digest().String()
	} else if err := write(cw); err != nil {
		return "", fmt.Errorf("failed to write diff: %w", err)
	}

	var commitopts []content.Opt
	if config.Labels != nil {
		commitopts = append(commitopts, content.WithLabels(config.Labels))
	}

	dgst := cw.Digest()
	if err := cw.Commit(ctx, 0, dgst, commitopts...); err != nil && !errdefs.IsAlreadyExists(err) {
		return "", fmt.Errorf("failed to commit: %w", err)
	}
	return dgst, nil
}

func uniqueRef() string {
	t := time.Now()
	var b [3]byte
	// Ignore read failures, just decreases uniqueness
	rand.Read(b[:])
	return fmt.Sprintf("%d-%s", t.UnixNano(), base64.URLEncoding.EncodeToString(b[:]))
}
