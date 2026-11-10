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

package server

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"

	"github.com/containerd/log"
)

func (c *criService) portForward(ctx context.Context, id string, port int32, stream io.ReadWriter) error {
	sandbox, err := c.sandboxStore.Get(id)
	if err != nil {
		return fmt.Errorf("failed to find sandbox %q in store: %w", id, err)
	}

	var podIP string
	if !hostNetwork(sandbox.Config) {
		// get ip address of the sandbox
		podIP, _, err = c.getIPs(sandbox)
		if err != nil {
			return fmt.Errorf("failed to get sandbox ip: %w", err)
		}
	} else {
		// HPCs use the host networking namespace.
		// Therefore, dial to localhost.
		podIP = "localhost"
	}

	err = func() error {
		var conn net.Conn
		conn, err = net.Dial("tcp", net.JoinHostPort(podIP, strconv.Itoa(int(port))))
		if err != nil {
			return fmt.Errorf("failed to connect to %s:%d for pod %q: %v", podIP, port, id, err)
		}
		log.G(ctx).Debugf("Connection to ip %s and port %d was successful", podIP, port)

		defer conn.Close()

		return forwardStream(ctx, id, port, stream, conn)
	}()

	if err != nil {
		return fmt.Errorf("failed to execute portforward for podId %v, podIp %v, err: %w", id, podIP, err)
	}
	log.G(ctx).Debugf("Finish port forwarding for windows %q port %d", id, port)

	return nil
}
