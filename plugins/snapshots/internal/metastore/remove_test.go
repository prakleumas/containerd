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

package metastore

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/containerd/containerd/v2/core/snapshots"
	"github.com/containerd/containerd/v2/core/snapshots/storage"
	"github.com/containerd/errdefs"
	bolt "go.etcd.io/bbolt"
)

// setup returns a metastore holding one active snapshot "key", and a
// function mapping a snapshot id to its path under a temporary directory.
func setup(t *testing.T, opts ...storage.Opt) (*storage.MetaStore, string, func(string) string) {
	t.Helper()
	root := t.TempDir()
	ms, err := storage.NewMetaStore(filepath.Join(root, "metadata.db"), opts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ms.Close() })

	var id string
	err = ms.WithTransaction(context.Background(), true, func(ctx context.Context) error {
		s, err := storage.CreateSnapshot(ctx, snapshots.KindActive, "key", "")
		id = s.ID
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return ms, id, func(id string) string { return filepath.Join(root, id) }
}

func keyExists(t *testing.T, ms *storage.MetaStore) bool {
	t.Helper()
	err := ms.WithTransaction(context.Background(), false, func(ctx context.Context) error {
		_, _, _, err := storage.GetInfo(ctx, "key")
		return err
	})
	if err != nil && !errdefs.IsNotFound(err) {
		t.Fatal(err)
	}
	return err == nil
}

func TestRemoveRenames(t *testing.T) {
	ms, id, pathOf := setup(t)
	if err := os.Mkdir(pathOf(id), 0700); err != nil {
		t.Fatal(err)
	}

	renamed, err := Remove(context.Background(), ms, "key", pathOf)
	if err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if want := pathOf("rm-" + id); renamed != want {
		t.Errorf("Remove() = %q, want %q", renamed, want)
	}
	if _, err := os.Stat(renamed); err != nil {
		t.Errorf("renamed path: %v", err)
	}
	if _, err := os.Stat(pathOf(id)); !os.IsNotExist(err) {
		t.Errorf("snapshot path still present after Remove: %v", err)
	}
	if keyExists(t, ms) {
		t.Error("snapshot metadata present after Remove")
	}
}

func TestRemoveMissingPath(t *testing.T) {
	ms, _, pathOf := setup(t)

	renamed, err := Remove(context.Background(), ms, "key", pathOf)
	if err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if renamed != "" {
		t.Errorf("Remove() = %q, want empty for a snapshot with no path", renamed)
	}
	if keyExists(t, ms) {
		t.Error("snapshot metadata present after Remove")
	}
}

func TestRemoveRenameFailureKeepsMetadata(t *testing.T) {
	ms, id, pathOf := setup(t)
	if err := os.Mkdir(pathOf(id), 0700); err != nil {
		t.Fatal(err)
	}
	// A non-empty directory at the rename target makes the rename fail.
	if err := os.MkdirAll(filepath.Join(pathOf("rm-"+id), "busy"), 0700); err != nil {
		t.Fatal(err)
	}

	renamed, err := Remove(context.Background(), ms, "key", pathOf)
	if err == nil {
		t.Fatalf("Remove() = %q, want error when the rename fails", renamed)
	}
	if renamed != "" {
		t.Errorf("Remove() = %q on error, want empty", renamed)
	}
	if _, err := os.Stat(pathOf(id)); err != nil {
		t.Errorf("snapshot path after failed Remove: %v", err)
	}
	if !keyExists(t, ms) {
		t.Error("snapshot metadata removed by a failed Remove")
	}
}

func TestRemoveCommitFailureRestoresPath(t *testing.T) {
	var dbFile *os.File
	ms, id, pathOf := setup(t, func(o *bolt.Options) error {
		o.OpenFile = func(name string, flag int, perm os.FileMode) (*os.File, error) {
			f, err := os.OpenFile(name, flag, perm)
			dbFile = f
			return f, err
		}
		return nil
	})
	if err := os.Mkdir(pathOf(id), 0700); err != nil {
		t.Fatal(err)
	}
	// Closing the database file inside the transaction makes its commit
	// fail after the rename has happened.
	closeDB := func(id string) string {
		dbFile.Close()
		return pathOf(id)
	}

	renamed, err := Remove(context.Background(), ms, "key", closeDB)
	if err == nil {
		t.Fatalf("Remove() = %q, want error when the commit fails", renamed)
	}
	if _, err := os.Stat(pathOf(id)); err != nil {
		t.Errorf("snapshot path after failed commit: %v", err)
	}
	if _, err := os.Stat(pathOf("rm-" + id)); !os.IsNotExist(err) {
		t.Errorf("rm- path after failed commit: %v", err)
	}
}
