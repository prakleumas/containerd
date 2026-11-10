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

package uniqueid

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

var idPattern = regexp.MustCompile(`^[0-9]+-[A-Za-z0-9_-]{4}$`)

func timestamp(t *testing.T, id string) int64 {
	t.Helper()
	if !idPattern.MatchString(id) {
		t.Fatalf("ID %q does not match %s", id, idPattern)
	}
	ts, err := strconv.ParseInt(id[:strings.IndexByte(id, '-')], 10, 64)
	if err != nil {
		t.Fatalf("ID %q: parsing timestamp: %v", id, err)
	}
	return ts
}

func TestNew(t *testing.T) {
	id := New()
	if ts := timestamp(t, id); ts >= int64(time.Second) {
		t.Errorf("New() = %q, want a timestamp below %d", id, int64(time.Second))
	}
}

func TestNewUnixNano(t *testing.T) {
	before := time.Now().UnixNano()
	id := NewUnixNano()
	after := time.Now().UnixNano()
	if ts := timestamp(t, id); ts < before || ts > after {
		t.Errorf("NewUnixNano() = %q, want a timestamp in [%d, %d]", id, before, after)
	}
}
