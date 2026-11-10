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
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/containerd/log"
	"github.com/moby/sys/mountinfo"
	runtimespec "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/opencontainers/selinux/go-selinux"
	"golang.org/x/sys/unix"
	runtime "k8s.io/cri-api/pkg/apis/runtime/v1"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/core/mount"
	"github.com/containerd/containerd/v2/core/snapshots"
	"github.com/containerd/containerd/v2/pkg/seccomp"
	"github.com/containerd/containerd/v2/pkg/sys"
)

const (
	// defaultSandboxOOMAdj is default omm adj for sandbox container. (kubernetes#47938).
	defaultSandboxOOMAdj = -998
	// defaultShmSize is the default size of the sandbox shm.
	defaultShmSize = int64(1024 * 1024 * 64)
	// relativeRootfsPath is the rootfs path relative to bundle path.
	relativeRootfsPath = "rootfs"
	// devShm is the default path of /dev/shm.
	devShm = "/dev/shm"
	// etcHosts is the default path of /etc/hosts file.
	etcHosts = "/etc/hosts"
	// resolvConfPath is the abs path of resolv.conf on host or container.
	resolvConfPath = "/etc/resolv.conf"
)

// getSandboxHostname returns the hostname file path inside the sandbox root directory.
func (c *Controller) getSandboxHostname(id string) string {
	return filepath.Join(c.getSandboxRootDir(id), "hostname")
}

// getSandboxHosts returns the hosts file path inside the sandbox root directory.
func (c *Controller) getSandboxHosts(id string) string {
	return filepath.Join(c.getSandboxRootDir(id), "hosts")
}

// getResolvPath returns resolv.conf filepath for specified sandbox.
func (c *Controller) getResolvPath(id string) string {
	return filepath.Join(c.getSandboxRootDir(id), "resolv.conf")
}

// getSandboxDevShm returns the shm file path inside the sandbox root directory.
func (c *Controller) getSandboxDevShm(id string) string {
	return filepath.Join(c.getVolatileSandboxRootDir(id), "shm")
}

// getSandboxPinnedNamespaces returns the pinned namespaces directory inside the
// sandbox state directory.
func (c *Controller) getSandboxPinnedNamespaces(id string) string {
	return filepath.Join(c.getVolatileSandboxRootDir(id), "pinned-namespaces")
}

// getSandboxPinnedUserNamespace returns the pinned user namespace file.
func (c *Controller) getSandboxPinnedUserNamespace(id string) string {
	return filepath.Join(c.getSandboxPinnedNamespaces(id), "user")
}

// pinUserNamespace persists user namespace in namespace filesystem.
func (c *Controller) pinUserNamespace(sandboxID string, netnsPath string) error {
	nsPath := c.getSandboxPinnedUserNamespace(sandboxID)

	baseDir := filepath.Dir(nsPath)
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return fmt.Errorf("failed to init pinned-namespaces directory %s: %w", baseDir, err)
	}

	emptyFd, err := os.OpenFile(nsPath, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0666)
	if err != nil {
		return fmt.Errorf("failed to create empty file %s: %w", nsPath, err)
	}
	emptyFd.Close()

	netnsFd, err := os.Open(netnsPath)
	if err != nil {
		return fmt.Errorf("failed to open netns(%s): %w", netnsPath, err)
	}
	defer netnsFd.Close()

	usernsFd, err := sys.GetUsernsForNamespace(netnsFd.Fd())
	if err != nil {
		return fmt.Errorf("failed to get user namespace for netns(%s): %w", netnsPath, err)
	}
	defer usernsFd.Close()

	if err = unix.Mount(usernsFd.Name(), nsPath, "none", unix.MS_BIND, ""); err != nil {
		return fmt.Errorf("failed to bind mount ns src: %v at %s: %w", usernsFd.Name(), nsPath, err)
	}
	return nil
}

func (c *Controller) seccompEnabled() bool {
	return seccomp.IsEnabled()
}

// unmountRecursive unmounts the target and all mounts underneath, starting with
// the deepest mount first.
func unmountRecursive(ctx context.Context, target string) error {
	target, err := mount.CanonicalizePath(target)
	if err != nil {
		return err
	}

	toUnmount, err := mountinfo.GetMounts(mountinfo.PrefixFilter(target))
	if err != nil {
		return err
	}

	// Make the deepest mount be first
	sort.Slice(toUnmount, func(i, j int) bool {
		return len(toUnmount[i].Mountpoint) > len(toUnmount[j].Mountpoint)
	})

	for i, m := range toUnmount {
		if err := mount.UnmountAll(m.Mountpoint, unix.MNT_DETACH); err != nil {
			if i == len(toUnmount)-1 { // last mount
				return err
			}
			// This is some submount, we can ignore this error for now, the final unmount will fail if this is a real problem
			log.G(ctx).WithError(err).Debugf("failed to unmount submount %s", m.Mountpoint)
		}
	}
	return nil
}

// EnsureRemoveAll wraps `os.RemoveAll` to check for specific errors that can
// often be remedied.
// Only use `EnsureRemoveAll` if you really want to make every effort to remove
// a directory.
//
// Because of the way `os.Remove` (and by extension `os.RemoveAll`) works, there
// can be a race between reading directory entries and then actually attempting
// to remove everything in the directory.
// These types of errors do not need to be returned since it's ok for the dir to
// be gone we can just retry the remove operation.
//
// This should not return a `os.ErrNotExist` kind of error under any circumstances
func EnsureRemoveAll(ctx context.Context, dir string) error {
	notExistErr := make(map[string]bool)

	// track retries
	exitOnErr := make(map[string]int)
	maxRetry := 50

	// Attempt to unmount anything beneath this dir first.
	if err := unmountRecursive(ctx, dir); err != nil {
		log.G(ctx).WithError(err).Debugf("failed to do initial unmount of %s", dir)
	}

	for {
		err := os.RemoveAll(dir)
		if err == nil {
			return nil
		}

		pe, ok := err.(*os.PathError)
		if !ok {
			return err
		}

		if os.IsNotExist(err) {
			if notExistErr[pe.Path] {
				return err
			}
			notExistErr[pe.Path] = true

			// There is a race where some subdir can be removed but after the
			// parent dir entries have been read.
			// So the path could be from `os.Remove(subdir)`
			// If the reported non-existent path is not the passed in `dir` we
			// should just retry, but otherwise return with no error.
			if pe.Path == dir {
				return nil
			}
			continue
		}

		if pe.Err != syscall.EBUSY {
			return err
		}
		if e := mount.Unmount(pe.Path, unix.MNT_DETACH); e != nil {
			return fmt.Errorf("error while removing %s: %w", dir, e)
		}

		if exitOnErr[pe.Path] == maxRetry {
			return err
		}
		exitOnErr[pe.Path]++
		time.Sleep(100 * time.Millisecond)
	}
}

var vmbasedRuntimes = []string{
	"io.containerd.kata",
}

func isVMBasedRuntime(runtimeType string) bool {
	for _, rt := range vmbasedRuntimes {
		if strings.Contains(runtimeType, rt) {
			return true
		}
	}
	return false
}

// ModifyProcessLabel sets the KVM SELinux process label on spec when the
// runtime is VM based and SELinux is enabled.
func ModifyProcessLabel(runtimeType string, spec *runtimespec.Spec) error {
	if !selinux.GetEnabled() || !isVMBasedRuntime(runtimeType) {
		return nil
	}
	l, err := selinux.SetProcessKind(spec.Process.SelinuxLabel, selinux.ProcessKindKVM)
	if err != nil {
		return fmt.Errorf("failed to get selinux kvm label: %w", err)
	}
	spec.Process.SelinuxLabel = l
	return nil
}

// SnapshotterRemapOpts returns the snapshotter options that remap the
// snapshot to the pod user namespace.
func SnapshotterRemapOpts(nsOpts *runtime.NamespaceOption) ([]snapshots.Opt, error) {
	snapshotOpt := []snapshots.Opt{}
	usernsOpts := nsOpts.GetUsernsOptions()
	if usernsOpts == nil {
		return snapshotOpt, nil
	}

	uids, gids, err := ParseUsernsIDs(usernsOpts)
	if err != nil {
		return nil, fmt.Errorf("user namespace configuration: %w", err)
	}

	if usernsOpts.GetMode() == runtime.NamespaceMode_POD {
		snapshotOpt = append(snapshotOpt, containerd.WithRemapperLabels(0, uids[0].HostID, 0, gids[0].HostID, uids[0].Size))
	}
	return snapshotOpt, nil
}
