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

package diff

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/Microsoft/go-winio"

	"github.com/containerd/log"
)

const processorPipe = "STREAM_PROCESSOR_PIPE"

// setupPayload serves data to the processor on a named pipe whose path is
// passed in the STREAM_PROCESSOR_PIPE environment variable. It returns no
// closer: the listener closes itself after the first connection.
func setupPayload(ctx context.Context, cmd *exec.Cmd, data []byte) (io.Closer, error) {
	up, err := getUiqPath()
	if err != nil {
		return nil, err
	}
	path := fmt.Sprintf("\\\\.\\pipe\\containerd-processor-%s-pipe", up)
	l, err := winio.ListenPipe(path, nil)
	if err != nil {
		return nil, err
	}
	go func() {
		defer l.Close()
		conn, err := l.Accept()
		if err != nil {
			log.G(ctx).WithError(err).Error("accept npipe connection")
			return
		}
		io.Copy(conn, bytes.NewReader(data))
		conn.Close()
	}()
	cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", processorPipe, path))
	return nil, nil
}

func getUiqPath() (string, error) {
	dir, err := os.MkdirTemp("", "")
	if err != nil {
		return "", err
	}
	os.Remove(dir)
	return filepath.Base(dir), nil
}
