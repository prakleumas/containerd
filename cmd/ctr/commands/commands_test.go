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

package commands

import "testing"

func TestFormatLabels(t *testing.T) {
	for _, tc := range []struct {
		name   string
		labels map[string]string
		want   string
	}{
		{name: "nil", labels: nil, want: ""},
		{name: "empty", labels: map[string]string{}, want: ""},
		// Pairs sort as whole "key=value" strings, so "a-b=2" ('-' < '=')
		// precedes "a=1".
		{name: "sorted pairs", labels: map[string]string{"a": "1", "c": "3", "a-b": "2"}, want: "a-b=2,a=1,c=3"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := FormatLabels(tc.labels); got != tc.want {
				t.Errorf("FormatLabels(%v) = %q, want %q", tc.labels, got, tc.want)
			}
		})
	}
}
