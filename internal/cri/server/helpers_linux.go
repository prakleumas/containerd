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
	"os"
	"path/filepath"

	"github.com/containerd/cgroups/v3"
	"golang.org/x/sys/unix"

	"github.com/containerd/containerd/v2/pkg/apparmor"
	"github.com/containerd/containerd/v2/pkg/seccomp"
)

// apparmorEnabled returns true if apparmor is enabled, supported by the host,
// if apparmor_parser is installed, and if we are not running docker-in-docker.
func (c *criService) apparmorEnabled() bool {
	if c.config.DisableApparmor {
		return false
	}
	return apparmor.HostSupports()
}

func (c *criService) seccompEnabled() bool {
	return seccomp.IsEnabled()
}

// openLogFile opens/creates a container log file.
func openLogFile(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	return os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY|unix.O_NOFOLLOW, 0640)
}

// getCgroupsMode returns cgropu mode.
// TODO: add build constraints to cgroups package and remove this helper
func isUnifiedCgroupsMode() bool {
	return cgroups.Mode() == cgroups.Unified
}
