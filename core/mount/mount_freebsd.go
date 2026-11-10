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

package mount

// Mount to the provided target.
//
// The "syscall" and "golang.org/x/sys/unix" packages do not define a Mount
// function for FreeBSD, so instead we execute mount(8) and trust it to do
// the right thing
func (m *Mount) mount(target string) error {
	// target: "/foo/target"
	// command: "mount -o ro -t nullfs /foo/source /foo/merged"
	// Note: FreeBSD mount(8) is particular about the order of flags and arguments
	var args []string
	for _, o := range m.Options {
		args = append(args, "-o", o)
	}
	args = append(args, "-t", m.Type)
	args = append(args, m.Source, target)

	return runMountHelper("mount", args, target)
}
