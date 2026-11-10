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

package process

import (
	"context"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/containerd/containerd/v2/pkg/namespaces"
)

func TestNewBinaryIO(t *testing.T) {
	ctx := namespaces.WithNamespace(context.Background(), "test")
	uri, _ := url.Parse("binary:///bin/echo?test")

	before := descriptorCount(t)

	io, err := NewBinaryIO(ctx, "1", uri)
	if err != nil {
		t.Fatal(err)
	}

	err = io.Close()
	if err != nil {
		t.Fatal(err)
	}

	after := descriptorCount(t)
	if before != after-1 { // one descriptor must be closed from shim logger side
		t.Fatalf("some descriptors weren't closed (%d != %d -1)", before, after)
	}
}

func TestNewBinaryIOBinaryV2Ready(t *testing.T) {
	ctx := namespaces.WithNamespace(context.Background(), "test")
	// Use a shell command that writes a byte to fd 5 (CONTAINER_WAIT) to signal readiness
	uri, _ := url.Parse("binary-v2:///bin/sh?-c=printf%20x%20%3E%265")

	before := descriptorCount(t)

	io, err := NewBinaryIO(ctx, "2", uri)
	if err != nil {
		t.Fatal(err)
	}

	err = io.Close()
	if err != nil {
		t.Fatal(err)
	}

	after := descriptorCount(t)
	if before != after-1 { // one descriptor must be closed from shim logger side
		t.Fatalf("some descriptors weren't closed (%d != %d -1)", before, after)
	}
}

func TestNewBinaryIOBinaryV2FailsWhenLoggerDoesNotSignalReady(t *testing.T) {
	ctx := namespaces.WithNamespace(context.Background(), "test")
	// Use /bin/true which exits without writing to fd 5 (CONTAINER_WAIT)
	uri, _ := url.Parse("binary-v2:///bin/true")

	before := descriptorCount(t)

	_, err := NewBinaryIO(ctx, "3", uri)
	if err == nil {
		t.Fatal("expected error when logger doesn't signal readiness")
	}
	if !strings.Contains(err.Error(), "did not call ready") {
		t.Fatalf("unexpected error: %v", err)
	}

	after := descriptorCount(t)
	if before != after {
		t.Fatalf("some descriptors weren't closed (%d != %d)", before, after)
	}
}

func TestNewBinaryIOCleanup(t *testing.T) {
	ctx := namespaces.WithNamespace(context.Background(), "test")
	uri, _ := url.Parse("binary:///not/existing")

	before := descriptorCount(t)
	_, err := NewBinaryIO(ctx, "2", uri)
	if err == nil {
		t.Fatal("error expected for invalid binary")
	}

	after := descriptorCount(t)
	if before != after {
		t.Fatalf("some descriptors weren't closed (%d != %d)", before, after)
	}
}

type readerIO struct {
	stdout, stderr io.ReadCloser
}

func (r *readerIO) Close() error          { return nil }
func (r *readerIO) Stdin() io.WriteCloser { return nil }
func (r *readerIO) Stdout() io.ReadCloser { return r.stdout }
func (r *readerIO) Stderr() io.ReadCloser { return r.stderr }
func (r *readerIO) Set(*exec.Cmd)         {}

func TestCopyPipesFiles(t *testing.T) {
	dir := t.TempDir()
	stdout := filepath.Join(dir, "stdout")
	stderr := filepath.Join(dir, "stderr")
	for _, name := range []string{stdout, stderr} {
		if err := os.WriteFile(name, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	rio := &readerIO{
		stdout: io.NopCloser(strings.NewReader("out")),
		stderr: io.NopCloser(strings.NewReader("err")),
	}

	var wg, cwg sync.WaitGroup
	if err := copyPipes(context.Background(), rio, "", stdout, stderr, &wg, &cwg); err != nil {
		t.Fatalf("copyPipes() = %v", err)
	}
	cwg.Wait()
	wg.Wait()

	for name, want := range map[string]string{stdout: "out", stderr: "err"} {
		got, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Errorf("copyPipes() wrote %q to %s, want %q", got, filepath.Base(name), want)
		}
	}
}

func descriptorCount(t *testing.T) int {
	t.Helper()
	const dir = "/proc/self/fd"
	files, _ := os.ReadDir(dir)

	// Go 1.23+ uses pidfd instead of PID for processes started by a user,
	// if possible (see https://go.dev/cl/570036). As a side effect, every
	// os.StartProcess or os.FindProcess call results in an extra opened
	// file descriptor, which is only closed in p.Wait or p.Release.
	//
	// To retain compatibility with previous Go versions (or Go 1.23+
	// behavior on older kernels), let's not count pidfds.
	//
	// TODO: if the proposal to check for internal file descriptors
	// (https://go.dev/issues/67639) is accepted, we can use that
	// instead to detect internal fds in use by the Go runtime.
	count := 0
	for _, file := range files {
		sym, err := os.Readlink(filepath.Join(dir, file.Name()))
		// Either pidfd:[70517] or anon_inode:[pidfd] (on Linux 5.4).
		if err == nil && strings.Contains(sym, "pidfd") {
			continue
		}
		count++
	}

	return count
}
