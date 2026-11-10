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

package pprof

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/containerd/containerd/v2/defaults"
	"github.com/urfave/cli/v3"
)

type pprofDialer struct {
	proto string
	addr  string
}

// Command is the cli command for providing golang pprof outputs for containerd
var Command = &cli.Command{
	Name:  "pprof",
	Usage: "Provide golang pprof outputs for containerd",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "debug-socket",
			Aliases: []string{"d"},
			Usage:   "Socket path for containerd's debug server",
			Value:   defaults.DefaultDebugAddress,
		},
	},
	DisableSliceFlagSeparator: true,
	Commands:                  Commands(getPProfClient),
}

// Client is a func that returns a http client for a pprof server
type Client func(cmd *cli.Command) (*http.Client, error)

// Commands returns the pprof subcommands, which fetch each profile from
// the pprof server reached through the client that clientFunc returns.
func Commands(clientFunc Client) []*cli.Command {
	profiles := []struct {
		name, usage, path string
		debug             uint
		seconds           time.Duration // zero for profiles without a duration
		secondsUsage      string
	}{
		{name: "block", usage: "Goroutine blocking profile", path: "block"},
		{name: "goroutines", usage: "Print goroutine stack dump", path: "goroutine", debug: 2},
		{name: "heap", usage: "Dump heap profile", path: "heap"},
		{name: "profile", usage: "CPU profile", path: "profile", seconds: 30 * time.Second, secondsUsage: "Duration for collection (seconds)"},
		{name: "threadcreate", usage: "Goroutine thread creating profile", path: "threadcreate"},
		{name: "trace", usage: "Collect execution trace", path: "trace", seconds: 5 * time.Second, secondsUsage: "Trace time (seconds)"},
	}
	cmds := make([]*cli.Command, 0, len(profiles))
	for _, p := range profiles {
		var flags []cli.Flag
		if p.seconds != 0 {
			flags = append(flags, &cli.DurationFlag{
				Name:    "seconds",
				Aliases: []string{"s"},
				Usage:   p.secondsUsage,
				Value:   p.seconds,
			})
		}
		flags = append(flags, &cli.UintFlag{
			Name:  "debug",
			Usage: "Output format, value = 0: binary, value > 0: plaintext",
			Value: p.debug,
		})
		cmds = append(cmds, &cli.Command{
			Name:                      p.name,
			Usage:                     p.usage,
			Flags:                     flags,
			DisableSliceFlagSeparator: true,
			Action: func(ctx context.Context, cmd *cli.Command) error {
				return dump(cmd, clientFunc, p.path, p.seconds != 0)
			},
		})
	}
	return cmds
}

// dump copies the profile at /debug/pprof/<path> to stdout.
func dump(cmd *cli.Command, clientFunc Client, path string, timed bool) error {
	client, err := clientFunc(cmd)
	if err != nil {
		return err
	}
	query := fmt.Sprintf("debug=%d", cmd.Uint("debug"))
	if timed {
		query = fmt.Sprintf("seconds=%v&%s", cmd.Duration("seconds").Seconds(), query)
	}
	output, err := httpGetRequest(client, "/debug/pprof/"+path+"?"+query)
	if err != nil {
		return err
	}
	defer output.Close()
	_, err = io.Copy(os.Stdout, output)
	return err
}

func getPProfClient(cmd *cli.Command) (*http.Client, error) {
	dialer := getPProfDialer(cmd.String("debug-socket"))

	tr := &http.Transport{
		DialContext: dialer.pprofDial,
	}
	client := &http.Client{Transport: tr}
	return client, nil
}

func httpGetRequest(client *http.Client, request string) (io.ReadCloser, error) {
	resp, err := client.Get("http://." + request)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("http get failed with status: %s", resp.Status)
	}
	return resp.Body, nil
}
