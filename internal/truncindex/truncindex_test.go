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

package truncindex

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"testing"
)

func TestGetFullID(t *testing.T) {
	idx := NewTruncIndex([]string{"abc", "abcdef", "xyz"})

	for _, id := range []string{"abc", "abcdef", "xyz"} {
		got, err := idx.Get(id)
		if err != nil || got != id {
			t.Errorf("Get(%q) = %q, %v; want %q, nil", id, got, err, id)
		}
	}

	if _, err := idx.Get("ab"); !errors.As(err, &ErrAmbiguousPrefix{}) {
		t.Errorf("Get(%q) error = %v; want ErrAmbiguousPrefix", "ab", err)
	}
}

func BenchmarkGetFullID(b *testing.B) {
	ids := make([]string, 500)
	for i := range ids {
		buf := make([]byte, 32)
		if _, err := rand.Read(buf); err != nil {
			b.Fatal(err)
		}
		ids[i] = hex.EncodeToString(buf)
	}
	idx := NewTruncIndex(ids)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := idx.Get(ids[i%len(ids)]); err != nil {
			b.Fatal(err)
		}
	}
}
