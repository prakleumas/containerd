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

// Package uniqueid generates short IDs for transient objects such as
// snapshot keys, content ingest references, mount IDs, and lease IDs.
package uniqueid

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"time"
)

// New returns an ID of the form "<nanoseconds>-<random>", where
// nanoseconds is the offset within the current second and random is
// three random bytes in URL-safe base64.
func New() string {
	return format(int64(time.Now().Nanosecond()))
}

// NewUnixNano returns an ID like [New] whose timestamp is the number
// of nanoseconds since the Unix epoch.
func NewUnixNano() string {
	return format(time.Now().UnixNano())
}

func format(ts int64) string {
	var b [3]byte
	rand.Read(b[:])
	return fmt.Sprintf("%d-%s", ts, base64.URLEncoding.EncodeToString(b[:]))
}
