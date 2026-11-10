//go:build !windows

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

package shim

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"

	"github.com/containerd/containerd/v2/cmd/ctr/commands/pprof"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/containerd/v2/pkg/shim"
	"github.com/urfave/cli/v3"
)

var pprofCommand = &cli.Command{
	Name:                      "pprof",
	Usage:                     "Provide golang pprof outputs for containerd-shim",
	Commands:                  pprof.Commands(getPProfClient),
	DisableSliceFlagSeparator: true,
}

func getPProfClient(cmd *cli.Command) (*http.Client, error) {
	id := cmd.String("id")
	if id == "" {
		return nil, errors.New("container id must be provided")
	}
	tr := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			ns := cmd.String("namespace")
			ctx = namespaces.WithNamespace(ctx, ns)
			s, _ := shim.SocketAddress(ctx, cmd.String("address"), id, true)
			s = strings.TrimPrefix(s, "unix://")
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", s)
		},
	}
	return &http.Client{Transport: tr}, nil
}
