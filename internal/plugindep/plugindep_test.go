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

package plugindep

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/containerd/plugin"
)

const (
	testType    plugin.Type = "io.containerd.test.v1"
	missingType plugin.Type = "io.containerd.missing.v1"
)

// newContext returns an InitContext whose set holds one plugin of testType
// per entry in instances, keyed by id.
func newContext(t *testing.T, instances map[string]any) *plugin.InitContext {
	t.Helper()
	set := plugin.NewPluginSet()
	ic := plugin.NewContext(context.Background(), set, nil)
	for id, inst := range instances {
		r := plugin.Registration{
			Type:   testType,
			ID:     id,
			InitFn: func(*plugin.InitContext) (any, error) { return inst, nil },
		}
		if err := set.Add(r.Init(ic)); err != nil {
			t.Fatal(err)
		}
	}
	return ic
}

// wantTypeError fails the test unless err names the plugin, the instance
// type and the requested type.
func wantTypeError(t *testing.T, call, pluginName string, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: got nil error, want a type mismatch error", call)
	}
	for _, name := range []string{pluginName, "int", "string"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("%s: error %q does not name %s", call, err, name)
		}
	}
}

func TestGetSingle(t *testing.T) {
	ic := newContext(t, map[string]any{"a": "value"})

	got, err := GetSingle[string](ic, testType)
	if err != nil || got != "value" {
		t.Errorf("GetSingle[string] = %q, %v; want %q, nil", got, err, "value")
	}

	_, err = GetSingle[int](ic, testType)
	wantTypeError(t, "GetSingle[int]", string(testType), err)

	_, err = GetSingle[string](ic, missingType)
	if !errors.Is(err, plugin.ErrPluginNotFound) {
		t.Errorf("GetSingle[string](%s) error = %v, want ErrPluginNotFound", missingType, err)
	}
}

func TestGetByID(t *testing.T) {
	ic := newContext(t, map[string]any{"a": "value"})

	got, err := GetByID[string](ic, testType, "a")
	if err != nil || got != "value" {
		t.Errorf("GetByID[string] = %q, %v; want %q, nil", got, err, "value")
	}

	_, err = GetByID[int](ic, testType, "a")
	wantTypeError(t, "GetByID[int]", string(testType)+".a", err)

	_, err = GetByID[string](ic, testType, "b")
	if !errors.Is(err, plugin.ErrPluginNotFound) {
		t.Errorf("GetByID[string](%s, b) error = %v, want ErrPluginNotFound", testType, err)
	}
}

func TestGetByType(t *testing.T) {
	ic := newContext(t, map[string]any{"a": "x", "b": "y"})

	got, err := GetByType[string](ic, testType)
	if err != nil || fmt.Sprint(got) != "map[a:x b:y]" {
		t.Errorf("GetByType[string] = %v, %v; want map[a:x b:y], nil", got, err)
	}

	got, err = GetByType[string](newContext(t, map[string]any{"a": "x", "b": 1}), testType)
	if got != nil {
		t.Errorf("GetByType[string] with a wrong-typed instance returned map %v, want nil", got)
	}
	wantTypeError(t, "GetByType[string]", string(testType)+".b", err)

	got, err = GetByType[string](ic, missingType)
	if got != nil || !errors.Is(err, plugin.ErrPluginNotFound) {
		t.Errorf("GetByType[string](%s) = %v, %v; want nil, ErrPluginNotFound", missingType, got, err)
	}
}
