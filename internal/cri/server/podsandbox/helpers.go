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

package podsandbox

import (
	"context"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"github.com/containerd/typeurl/v2"
	runtimespec "github.com/opencontainers/runtime-spec/specs-go"
	runtime "k8s.io/cri-api/pkg/apis/runtime/v1"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/core/containers"
	crilabels "github.com/containerd/containerd/v2/internal/cri/labels"
	sandboxstore "github.com/containerd/containerd/v2/internal/cri/store/sandbox"
	ctrdutil "github.com/containerd/containerd/v2/internal/cri/util"
	"github.com/containerd/containerd/v2/pkg/oci"
)

const (

	// sandboxesDir contains all sandbox root. A sandbox root is the running
	// directory of the sandbox, all files created for the sandbox will be
	// placed under this directory.
	sandboxesDir = "sandboxes"
	// MetadataKey is the key used for storing metadata in the sandbox extensions
	MetadataKey = "metadata"
	// UpdatedResourcesKey is the key for the sandbox extension storing updated resources.
	UpdatedResourcesKey = "updated-resources"
)

const (
	// unknownExitCode is the exit code when exit reason is unknown.
	unknownExitCode = 255
)

// UpdatedResources holds the updated Linux resource constraints for a PodSandbox.
type UpdatedResources struct {
	Overhead  *runtime.LinuxContainerResources
	Resources *runtime.LinuxContainerResources
}

func init() {
	typeurl.Register(&UpdatedResources{}, "io.containerd.cri.v1", "UpdatedResources")
}

// getSandboxRootDir returns the root directory for managing sandbox files,
// e.g. hosts files.
func (c *Controller) getSandboxRootDir(id string) string {
	return filepath.Join(c.config.RootDir, sandboxesDir, id)
}

// getVolatileSandboxRootDir returns the root directory for managing volatile sandbox files,
// e.g. named pipes.
func (c *Controller) getVolatileSandboxRootDir(id string) string {
	return filepath.Join(c.config.StateDir, sandboxesDir, id)
}

// runtimeSpec returns a default runtime spec used in cri-containerd.
func (c *Controller) runtimeSpec(id string, opts ...oci.SpecOpts) (*runtimespec.Spec, error) {
	// GenerateSpec needs namespace.
	ctx := ctrdutil.NamespacedContext()
	container := &containers.Container{ID: id}

	spec, err := oci.GenerateSpec(ctx, nil, container, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to generate spec: %w", err)
	}

	return spec, nil
}

func getMetadata(ctx context.Context, container containerd.Container) (*sandboxstore.Metadata, error) {
	// Load sandbox metadata.
	exts, err := container.Extensions(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get sandbox container extensions: %w", err)
	}
	ext, ok := exts[crilabels.SandboxMetadataExtension]
	if !ok {
		return nil, fmt.Errorf("metadata extension %q not found", crilabels.SandboxMetadataExtension)
	}
	data, err := typeurl.UnmarshalAny(ext)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal metadata extension %q: %w", ext, err)
	}
	meta, ok := data.(*sandboxstore.Metadata)
	if !ok {
		return nil, errors.New("failed to convert the extension to sandbox metadata")
	}
	return meta, nil
}

// GetCgroupsPath generates container cgroups path.
func GetCgroupsPath(cgroupsParent, id string) string {
	base := path.Base(cgroupsParent)
	if strings.HasSuffix(base, ".slice") {
		// For a.slice/b.slice/c.slice, base is c.slice.
		// runc systemd cgroup path format is "slice:prefix:name".
		return strings.Join([]string{base, "cri-containerd", id}, ":")
	}
	return filepath.Join(cgroupsParent, id)
}

// ParseUsernsIDMap converts a CRI ID mapping list into an OCI ID mapping list.
func ParseUsernsIDMap(runtimeIDMap []*runtime.IDMapping) ([]runtimespec.LinuxIDMapping, error) {
	var m []runtimespec.LinuxIDMapping

	if len(runtimeIDMap) == 0 {
		return m, nil
	}

	if len(runtimeIDMap) > 1 {
		// We only accept 1 line, because containerd.WithRemappedSnapshot() only supports that.
		return m, fmt.Errorf("only one mapping line supported, got %v mapping lines", len(runtimeIDMap))
	}

	// We know len is 1 now.
	if runtimeIDMap[0] == nil {
		return m, nil
	}
	uidMap := runtimeIDMap[0]

	if uidMap.Length < 1 {
		return m, fmt.Errorf("invalid mapping length: %v", uidMap.Length)
	}

	m = []runtimespec.LinuxIDMapping{
		{
			ContainerID: uidMap.ContainerId,
			HostID:      uidMap.HostId,
			Size:        uidMap.Length,
		},
	}

	return m, nil
}

// ParseUsernsIDs returns the UID and GID mappings of a CRI user namespace.
func ParseUsernsIDs(userns *runtime.UserNamespace) (uids, gids []runtimespec.LinuxIDMapping, retErr error) {
	if userns == nil {
		// If userns is not set, the kubelet doesn't support this option
		// and we should just fallback to no userns. This is completely
		// valid.
		return nil, nil, nil
	}

	uids, err := ParseUsernsIDMap(userns.GetUids())
	if err != nil {
		return nil, nil, fmt.Errorf("UID mapping: %w", err)
	}

	gids, err = ParseUsernsIDMap(userns.GetGids())
	if err != nil {
		return nil, nil, fmt.Errorf("GID mapping: %w", err)
	}

	switch mode := userns.GetMode(); mode {
	case runtime.NamespaceMode_NODE:
		if len(uids) != 0 || len(gids) != 0 {
			return nil, nil, fmt.Errorf("can't use user namespace mode %q with mappings. Got %v UID mappings and %v GID mappings", mode, len(uids), len(gids))
		}
	case runtime.NamespaceMode_POD:
		// This is valid, we will handle it in WithPodNamespaces().
		if len(uids) == 0 || len(gids) == 0 {
			return nil, nil, fmt.Errorf("can't use user namespace mode %q without UID and GID mappings", mode)
		}
	default:
		return nil, nil, fmt.Errorf("unsupported user namespace mode: %q", mode)
	}

	return uids, gids, nil
}
