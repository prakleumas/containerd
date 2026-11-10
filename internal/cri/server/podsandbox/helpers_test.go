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
	"os"
	goruntime "runtime"
	"testing"

	"github.com/containerd/containerd/v2/core/containers"
	"github.com/containerd/containerd/v2/pkg/oci"
	"github.com/containerd/containerd/v2/pkg/protobuf/types"
	"github.com/containerd/typeurl/v2"
	runtimespec "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	runtime "k8s.io/cri-api/pkg/apis/runtime/v1"
)

func TestEnvDeduplication(t *testing.T) {
	for _, test := range []struct {
		desc     string
		existing []string
		kv       [][2]string
		expected []string
	}{
		{
			desc: "single env",
			kv: [][2]string{
				{"a", "b"},
			},
			expected: []string{"a=b"},
		},
		{
			desc: "multiple envs",
			kv: [][2]string{
				{"a", "b"},
				{"c", "d"},
				{"e", "f"},
			},
			expected: []string{
				"a=b",
				"c=d",
				"e=f",
			},
		},
		{
			desc: "env override",
			kv: [][2]string{
				{"k1", "v1"},
				{"k2", "v2"},
				{"k3", "v3"},
				{"k3", "v4"},
				{"k1", "v5"},
				{"k4", "v6"},
			},
			expected: []string{
				"k1=v5",
				"k2=v2",
				"k3=v4",
				"k4=v6",
			},
		},
		{
			desc: "existing env",
			existing: []string{
				"k1=v1",
				"k2=v2",
				"k3=v3",
			},
			kv: [][2]string{
				{"k3", "v4"},
				{"k2", "v5"},
				{"k4", "v6"},
			},
			expected: []string{
				"k1=v1",
				"k2=v5",
				"k3=v4",
				"k4=v6",
			},
		},
	} {
		t.Run(test.desc, func(t *testing.T) {
			var spec runtimespec.Spec
			if len(test.existing) > 0 {
				spec.Process = &runtimespec.Process{
					Env: test.existing,
				}
			}
			for _, kv := range test.kv {
				oci.WithEnv([]string{kv[0] + "=" + kv[1]})(context.Background(), nil, nil, &spec)
			}
			assert.Equal(t, test.expected, spec.Process.Env)
		})
	}
}

func TestEnsureRemoveAllNotExist(t *testing.T) {
	// should never return an error for a non-existent path
	if err := EnsureRemoveAll(context.Background(), "/non/existent/path"); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureRemoveAllWithDir(t *testing.T) {
	dir := t.TempDir()
	if err := EnsureRemoveAll(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureRemoveAllWithFile(t *testing.T) {
	tmp, err := os.CreateTemp(t.TempDir(), "test-ensure-removeall-with-dir")
	require.NoError(t, err)
	tmp.Close()

	err = EnsureRemoveAll(context.Background(), tmp.Name())
	require.NoError(t, err)
}

func TestGetRuntimeOptions(t *testing.T) {
	_, err := GetRuntimeOptions(containers.Container{})
	require.NoError(t, err)

	var pbany *types.Any               // This is nil.
	var typeurlAny typeurl.Any = pbany // This is typed nil.
	_, err = GetRuntimeOptions(containers.Container{Runtime: containers.RuntimeInfo{Options: typeurlAny}})
	require.NoError(t, err)
}

func TestHostNetwork(t *testing.T) {
	tests := []struct {
		name     string
		c        *runtime.PodSandboxConfig
		expected bool
	}{
		{
			name: "when pod namespace return false",
			c: &runtime.PodSandboxConfig{
				Linux: &runtime.LinuxPodSandboxConfig{
					SecurityContext: &runtime.LinuxSandboxSecurityContext{
						NamespaceOptions: &runtime.NamespaceOption{
							Network: runtime.NamespaceMode_POD,
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "when node namespace return true",
			c: &runtime.PodSandboxConfig{
				Linux: &runtime.LinuxPodSandboxConfig{
					SecurityContext: &runtime.LinuxSandboxSecurityContext{
						NamespaceOptions: &runtime.NamespaceOption{
							Network: runtime.NamespaceMode_NODE,
						},
					},
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		if goruntime.GOOS != "linux" {
			t.Skip()
		}

		t.Run(tt.name, func(t *testing.T) {
			if HostNetwork(tt.c) != tt.expected {
				t.Errorf("failed HostNetwork got %t expected %t", HostNetwork(tt.c), tt.expected)
			}
		})
	}
}
