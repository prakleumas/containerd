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

package server

import (
	"testing"
	"time"

	runtime "k8s.io/cri-api/pkg/apis/runtime/v1"

	runcoptions "github.com/containerd/containerd/api/types/runc/options"
	criconfig "github.com/containerd/containerd/v2/internal/cri/config"
	containerstore "github.com/containerd/containerd/v2/internal/cri/store/container"
	"github.com/containerd/containerd/v2/plugins"

	runtimespec "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/pelletier/go-toml/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetUserFromImage tests the logic of getting image uid or user name of image user.
func TestGetUserFromImage(t *testing.T) {
	newI64 := func(i int64) *int64 { return &i }
	for _, test := range []struct {
		desc string
		user string
		uid  *int64
		name string
	}{
		{
			desc: "no gid",
			user: "0",
			uid:  newI64(0),
		},
		{
			desc: "uid/gid",
			user: "0:1",
			uid:  newI64(0),
		},
		{
			desc: "empty user",
			user: "",
		},
		{
			desc: "multiple separators",
			user: "1:2:3",
			uid:  newI64(1),
		},
		{
			desc: "root username",
			user: "root:root",
			name: "root",
		},
		{
			desc: "username",
			user: "test:test",
			name: "test",
		},
	} {
		t.Run(test.desc, func(t *testing.T) {
			actualUID, actualName := getUserFromImage(test.user)
			assert.Equal(t, test.uid, actualUID)
			assert.Equal(t, test.name, actualName)
		})
	}
}

func TestGenerateRuntimeOptions(t *testing.T) {
	nilOpts := `
systemd_cgroup = true
[containerd]
  no_pivot = true
  default_runtime_name = "default"
[containerd.runtimes.runcv2]
  runtime_type = "` + plugins.RuntimeRuncV2 + `"
`
	nonNilOpts := `
systemd_cgroup = true
[containerd]
  no_pivot = true
  default_runtime_name = "default"
[containerd.runtimes.legacy.options]
  Runtime = "legacy"
  RuntimeRoot = "/legacy"
[containerd.runtimes.runc.options]
  BinaryName = "runc"
  Root = "/runc"
  NoNewKeyring = true
[containerd.runtimes.runcv2]
  runtime_type = "` + plugins.RuntimeRuncV2 + `"
[containerd.runtimes.runcv2.options]
  BinaryName = "runc"
  Root = "/runcv2"
  NoNewKeyring = true
`
	var nilOptsConfig, nonNilOptsConfig criconfig.Config
	err := toml.Unmarshal([]byte(nilOpts), &nilOptsConfig)
	require.NoError(t, err)
	require.Len(t, nilOptsConfig.Runtimes, 1)

	err = toml.Unmarshal([]byte(nonNilOpts), &nonNilOptsConfig)
	require.NoError(t, err)
	require.Len(t, nonNilOptsConfig.Runtimes, 3)

	for _, test := range []struct {
		desc            string
		r               criconfig.Runtime
		c               criconfig.Config
		expectedOptions any
	}{
		{
			desc:            "when options is nil, should return nil option for io.containerd.runc.v2",
			r:               nilOptsConfig.Runtimes["runcv2"],
			c:               nilOptsConfig,
			expectedOptions: nil,
		},
		{
			desc: "when options is not nil, should be able to decode for io.containerd.runc.v2",
			r:    nonNilOptsConfig.Runtimes["runcv2"],
			c:    nonNilOptsConfig,
			expectedOptions: &runcoptions.Options{
				BinaryName:   "runc",
				Root:         "/runcv2",
				NoNewKeyring: true,
			},
		},
	} {
		t.Run(test.desc, func(t *testing.T) {
			opts, err := criconfig.GenerateRuntimeOptions(test.r)
			assert.NoError(t, err)
			assert.Equal(t, test.expectedOptions, opts)
		})
	}
}

// Helper function for setting up an environment to test PID namespace targeting.
func addContainer(c *criService, containerID, sandboxID string, PID uint32, createdAt, startedAt, finishedAt int64) error {
	meta := containerstore.Metadata{
		ID:        containerID,
		SandboxID: sandboxID,
	}
	status := containerstore.Status{
		Pid:        PID,
		CreatedAt:  createdAt,
		StartedAt:  startedAt,
		FinishedAt: finishedAt,
	}
	container, err := containerstore.NewContainer(meta,
		containerstore.WithFakeStatus(status),
	)
	if err != nil {
		return err
	}
	return c.containerStore.Add(container)
}

func TestValidateTargetContainer(t *testing.T) {
	testSandboxID := "test-sandbox-uid"

	// The existing container that will be targeted.
	testTargetContainerID := "test-target-container"
	testTargetContainerPID := uint32(4567)

	// A container that has finished running and cannot be targeted.
	testStoppedContainerID := "stopped-target-container"
	testStoppedContainerPID := uint32(6789)

	// A container from another pod.
	testOtherContainerSandboxID := "other-sandbox-uid"
	testOtherContainerID := "other-target-container"
	testOtherContainerPID := uint32(7890)

	// Container create/start/stop times.
	createdAt := time.Now().Add(-15 * time.Second).UnixNano()
	startedAt := time.Now().Add(-10 * time.Second).UnixNano()
	finishedAt := time.Now().Add(-5 * time.Second).UnixNano()

	c := newTestCRIService()

	// Create a target container.
	err := addContainer(c, testTargetContainerID, testSandboxID, testTargetContainerPID, createdAt, startedAt, 0)
	require.NoError(t, err, "error creating test target container")

	// Create a stopped container.
	err = addContainer(c, testStoppedContainerID, testSandboxID, testStoppedContainerPID, createdAt, startedAt, finishedAt)
	require.NoError(t, err, "error creating test stopped container")

	// Create a container in another pod.
	err = addContainer(c, testOtherContainerID, testOtherContainerSandboxID, testOtherContainerPID, createdAt, startedAt, 0)
	require.NoError(t, err, "error creating test container in other pod")

	for _, test := range []struct {
		desc              string
		targetContainerID string
		expectError       bool
	}{
		{
			desc:              "target container in pod",
			targetContainerID: testTargetContainerID,
			expectError:       false,
		},
		{
			desc:              "target stopped container in pod",
			targetContainerID: testStoppedContainerID,
			expectError:       true,
		},
		{
			desc:              "target container does not exist",
			targetContainerID: "no-container-with-this-id",
			expectError:       true,
		},
		{
			desc:              "target container in other pod",
			targetContainerID: testOtherContainerID,
			expectError:       true,
		},
	} {
		t.Run(test.desc, func(t *testing.T) {
			targetContainer, err := c.validateTargetContainer(testSandboxID, test.targetContainerID)
			if test.expectError {
				require.Error(t, err, "target should have been invalid but no error")
				return
			}
			require.NoErrorf(t, err, "target should have been valid but got error")

			assert.Equal(t, test.targetContainerID, targetContainer.ID, "returned target container does not have expected ID")
		})
	}

}

func TestCopyResourcesToStatusWindowsAffinity(t *testing.T) {
	spec := &runtimespec.Spec{
		Windows: &runtimespec.Windows{
			Resources: &runtimespec.WindowsResources{
				CPU: &runtimespec.WindowsCPUResources{
					Affinity: []runtimespec.WindowsCPUGroupAffinity{
						{Mask: 0x3, Group: 0},
						{Mask: 0xf, Group: 1},
					},
				},
			},
		},
	}
	status := copyResourcesToStatus(spec, containerstore.Status{})
	require.NotNil(t, status.Resources)
	require.NotNil(t, status.Resources.Windows)
	assert.Equal(t, []*runtime.WindowsCpuGroupAffinity{
		{CpuMask: 0x3, CpuGroup: 0},
		{CpuMask: 0xf, CpuGroup: 1},
	}, status.Resources.Windows.AffinityCpus)
}
