//go:build linux

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

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"

	"github.com/containerd/cgroups/v3"
	bootapi "github.com/containerd/containerd/api/runtime/bootstrap/v1"
	"github.com/containerd/containerd/api/types"
	"github.com/containerd/containerd/api/types/runc/options"
	"github.com/containerd/errdefs"
	"github.com/containerd/typeurl/v2"
	"github.com/opencontainers/runtime-spec/specs-go/features"

	legacymanager "github.com/containerd/containerd/v2/cmd/containerd-shim-runc-v2/manager"
	"github.com/containerd/containerd/v2/cmd/containerd-shim-runc-v2/runc"
	"github.com/containerd/containerd/v2/cmd/containerd-shim-sandboxed-runc-v2/sandbox"
	"github.com/containerd/containerd/v2/pkg/shim"
	"github.com/containerd/containerd/v2/plugins"
)

// manager adapts the containerd-shim-runc-v2 manager to the sandbox flow:
// "start" runs once per pod, for the sandbox, keyed by the sandbox id
// (containers join the shim through the sandbox endpoint, so there is no
// grouping by OCI annotations); "delete" also cleans up a sandbox bundle; and
// "-info" hides the user namespace feature.
type manager struct {
	// legacy is the containerd-shim-runc-v2 manager. It serves the runtime
	// info (the same runtime type, the same runc, the same features) and the
	// cleanup of the task bundles joined to this shim.
	legacy shim.Shim
}

func newManager() shim.Shim {
	return &manager{legacy: legacymanager.NewShimManager(plugins.RuntimeRuncV2)}
}

// Name is the runtime type served: containerd reaches this binary through
// runtime_path, not through a runtime type of its own.
func (m *manager) Name() string {
	return m.legacy.Name()
}

// Start starts the shim process for a sandbox bundle. containerd calls it once
// per sandbox; the containers of the pod join the running shim through the
// bootstrap protocol without invoking "start" again.
func (m *manager) Start(ctx context.Context, opts *bootapi.BootstrapParams) (*bootapi.BootstrapResult, error) {
	if cgroups.Mode() != cgroups.Unified {
		return nil, fmt.Errorf("%s requires cgroup v2 (the unified hierarchy) and this host runs cgroup v1: %w", sandbox.BinaryName, errdefs.ErrNotImplemented)
	}

	// The working directory is the sandbox bundle.
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	id := opts.GetInstanceID()
	return legacymanager.Start(ctx, opts, legacymanager.StartConfig{
		// The socket is keyed by the sandbox id alone.
		Grouping: id,
		Env:      []string{"OTEL_SERVICE_NAME=containerd-shim-" + id},
		OnRuncOptions: func(runcOpts *options.Options) error {
			// The runc options are persisted in the bundle. The sandbox service
			// reads them to run containerd-sandbox-init through the same runc,
			// root and cgroup driver as the containers of the pod, and Stop
			// reads them to delete that container once the shim is gone.
			if err := runc.WriteOptions(cwd, runcOpts); err != nil {
				return fmt.Errorf("failed to persist the runc options: %w", err)
			}
			if err := runc.WriteRuntime(cwd, runcOpts.GetBinaryName()); err != nil {
				return fmt.Errorf("failed to persist the runtime name: %w", err)
			}
			return nil
		},
	})
}

// Stop is "shim delete". containerd runs it in a bundle once the shim that
// served the bundle is gone: in the sandbox bundle, and in the bundle of every
// container task that joined the shim. Stop does not tell the two apart and
// runs both cleanups every time:
//   - The runc task cleanup of containerd-shim-runc-v2 force-deletes the runc
//     container of that id and detaches its rootfs. In the sandbox bundle of a
//     shared-PID pod that deletes containerd-sandbox-init; in any other
//     sandbox bundle it does nothing.
//   - The sandbox cleanup detaches every mount under the bundle and removes
//     the pod files. In a task bundle it does nothing.
func (m *manager) Stop(ctx context.Context, id string) (shim.StopStatus, error) {
	status, err := m.legacy.Stop(ctx, id)
	cwd, cwdErr := os.Getwd()
	if cwdErr != nil {
		return status, errors.Join(err, cwdErr)
	}
	return status, errors.Join(err, sandbox.Cleanup(ctx, filepath.Join(filepath.Dir(cwd), id)))
}

// Info returns the runtime info of containerd-shim-runc-v2: the same runtime
// type, and the features of the same runc minus the user namespace. CRI
// advertises user namespace support for a handler from that feature, and this
// shim rejects user namespaced pods for now.
func (m *manager) Info(ctx context.Context, optionsR io.Reader) (*types.RuntimeInfo, error) {
	info, err := m.legacy.Info(ctx, optionsR)
	if err != nil || info.GetFeatures() == nil {
		return info, err
	}
	var feat features.Features
	if err := typeurl.UnmarshalTo(info.Features, &feat); err != nil {
		return nil, fmt.Errorf("failed to decode the runc features: %w", err)
	}
	if feat.Linux != nil {
		feat.Linux.Namespaces = slices.DeleteFunc(feat.Linux.Namespaces, func(ns string) bool { return ns == "user" })
	}
	if info.Features, err = typeurl.MarshalAnyToProto(&feat); err != nil {
		return nil, fmt.Errorf("failed to encode the runc features: %w", err)
	}
	return info, nil
}
