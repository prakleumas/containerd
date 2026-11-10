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

package diff

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/containerd/containerd/v2/pkg/protobuf/proto"
	"github.com/containerd/typeurl/v2"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestBinaryProcessorPayload(t *testing.T) {
	payload, err := typeurl.MarshalAny(wrapperspb.String("secret"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := proto.Marshal(typeurl.MarshalProto(payload))
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	stream := NewProcessorChain("in/type", strings.NewReader("layer"))
	script := `printf %s "$STREAM_PROCESSOR_MEDIATYPE"; cat; cat <&3`
	p, err := NewBinaryProcessor(ctx, "in/type", "out/type", stream, "/bin/sh", []string{"-c", script}, nil, payload)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	got, err := io.ReadAll(p)
	if err != nil {
		t.Fatal(err)
	}
	if want := "in/type" + "layer" + string(data); string(got) != want {
		t.Errorf("NewBinaryProcessor output = %q, want %q", got, want)
	}
	if err := p.(*binaryProcessor).Wait(ctx); err != nil {
		t.Errorf("Wait() = %v", err)
	}
	if mt := p.MediaType(); mt != "out/type" {
		t.Errorf("MediaType() = %q, want %q", mt, "out/type")
	}
}
