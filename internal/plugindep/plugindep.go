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

// Package plugindep looks up the plugins that a plugin depends on and
// returns their instances as the Go type the caller expects.
package plugindep

import (
	"fmt"
	"reflect"

	"github.com/containerd/plugin"
)

// GetSingle returns the single initialized plugin of type t as a T.
// Lookup errors from [plugin.InitContext.GetSingle] are returned unchanged.
func GetSingle[T any](ic *plugin.InitContext, t plugin.Type) (T, error) {
	p, err := ic.GetSingle(t)
	if err != nil {
		var zero T
		return zero, err
	}
	return as[T](p, t.String())
}

// GetByID returns the plugin of type t with the given id as a T.
// Lookup errors from [plugin.InitContext.GetByID] are returned unchanged.
func GetByID[T any](ic *plugin.InitContext, t plugin.Type, id string) (T, error) {
	p, err := ic.GetByID(t, id)
	if err != nil {
		var zero T
		return zero, err
	}
	return as[T](p, t.String()+"."+id)
}

// GetByType returns all initialized plugins of type t, keyed by id, as Ts.
// Lookup errors from [plugin.InitContext.GetByType] are returned unchanged.
// The map is nil whenever the error is non-nil.
func GetByType[T any](ic *plugin.InitContext, t plugin.Type) (map[string]T, error) {
	ps, err := ic.GetByType(t)
	if err != nil {
		return nil, err
	}
	m := make(map[string]T, len(ps))
	for id, p := range ps {
		v, err := as[T](p, t.String()+"."+id)
		if err != nil {
			return nil, err
		}
		m[id] = v
	}
	return m, nil
}

func as[T any](p any, name string) (T, error) {
	v, ok := p.(T)
	if !ok {
		return v, fmt.Errorf("plugin %s has type %T, want %s", name, p, reflect.TypeFor[T]())
	}
	return v, nil
}
