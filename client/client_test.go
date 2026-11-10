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

package client

import (
	"context"
	"errors"
	"testing"

	"github.com/containerd/containerd/v2/defaults"
	"github.com/containerd/containerd/v2/pkg/namespaces"
)

// labelNamespaces is a namespaces.Store whose Labels returns labels, or err.
type labelNamespaces struct {
	namespaces.Store
	labels map[string]string
	err    error
}

func (s *labelNamespaces) Labels(context.Context, string) (map[string]string, error) {
	return s.labels, s.err
}

func TestDefaultRuntimeAndSandboxer(t *testing.T) {
	errLabels := errors.New("labels unavailable")
	labels := map[string]string{
		defaults.DefaultRuntimeNSLabel:   "label-runtime",
		defaults.DefaultSandboxerNSLabel: "label-sandboxer",
	}
	for _, tc := range []struct {
		name          string
		opts          []Opt
		store         *labelNamespaces
		wantRuntime   string
		wantSandboxer string
		wantErr       error
	}{
		{
			name:          "option",
			opts:          []Opt{WithDefaultRuntime("opt-runtime"), WithDefaultSandboxer("opt-sandboxer")},
			store:         &labelNamespaces{labels: labels},
			wantRuntime:   "opt-runtime",
			wantSandboxer: "opt-sandboxer",
		},
		{
			name:          "label",
			store:         &labelNamespaces{labels: labels},
			wantRuntime:   "label-runtime",
			wantSandboxer: "label-sandboxer",
		},
		{
			name:          "no label",
			store:         &labelNamespaces{},
			wantRuntime:   defaults.DefaultRuntime,
			wantSandboxer: defaults.DefaultSandboxer,
		},
		{
			name:          "label error",
			store:         &labelNamespaces{err: errLabels},
			wantRuntime:   defaults.DefaultRuntime,
			wantSandboxer: defaults.DefaultSandboxer,
			wantErr:       errLabels,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := append([]Opt{
				WithDefaultNamespace("test"),
				WithServices(WithNamespaceService(tc.store)),
			}, tc.opts...)
			c, err := New("", opts...)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			ctx := context.Background()
			for _, get := range []struct {
				name string
				fn   func(context.Context) (string, error)
				want string
			}{
				{"defaultRuntime", c.defaultRuntime, tc.wantRuntime},
				{"defaultSandboxer", c.defaultSandboxer, tc.wantSandboxer},
			} {
				// The second call checks that a failed lookup did not keep the fallback.
				for range 2 {
					got, err := get.fn(ctx)
					if got != get.want || !errors.Is(err, tc.wantErr) {
						t.Errorf("%s() = %q, %v; want %q, %v", get.name, got, err, get.want, tc.wantErr)
					}
				}
			}
		})
	}
}
