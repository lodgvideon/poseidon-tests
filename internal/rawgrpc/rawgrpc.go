// Package rawgrpc carries the gRPC pieces shared by the target server and the
// grpc-go client arm: a pass-through codec and a hand-written ServiceDesc.
//
// The benchmark deliberately does not use protobuf. Both gRPC arms exchange
// the *same raw JSON bytes* as every HTTP regime, so the payload is constant
// across all four regimes and `protoc` stays out of the build. poseidon's gRPC
// client is natively []byte-oriented; on the grpc-go side we get the same
// behaviour with a codec that hands bytes straight through. See ADR-0003.
package rawgrpc

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/encoding"
)

// CodecName is the content-subtype both sides negotiate.
const CodecName = "rawbytes"

// ServiceName and method names, matching what poseidon's client will send as
// the :path pseudo-header (/<service>/<method>).
const (
	ServiceName = "bench.Bench"
	MethodEcho  = "Echo"

	// FullMethodEcho is the on-the-wire path for the Echo method.
	FullMethodEcho = "/" + ServiceName + "/" + MethodEcho
)

// Codec passes []byte payloads through untouched in both directions.
type Codec struct{}

// Marshal implements encoding.Codec.
func (Codec) Marshal(v any) ([]byte, error) {
	b, ok := v.(*[]byte)
	if !ok {
		return nil, fmt.Errorf("rawgrpc: Marshal wants *[]byte, got %T", v)
	}
	return *b, nil
}

// Unmarshal implements encoding.Codec.
func (Codec) Unmarshal(data []byte, v any) error {
	b, ok := v.(*[]byte)
	if !ok {
		return fmt.Errorf("rawgrpc: Unmarshal wants *[]byte, got %T", v)
	}
	// grpc-go reuses its read buffer, so the payload must be copied out.
	*b = append((*b)[:0], data...)
	return nil
}

// Name implements encoding.Codec.
func (Codec) Name() string { return CodecName }

func init() { encoding.RegisterCodec(Codec{}) }

// EchoHandler is the server-side implementation of Echo: it receives the
// request bytes and returns the response bytes.
type EchoHandler func(ctx context.Context, req []byte) ([]byte, error)

// ServiceDesc builds the grpc.ServiceDesc for the bench service, wired to h.
// Written by hand because there is no generated protobuf stub.
func ServiceDesc(h EchoHandler) *grpc.ServiceDesc {
	return &grpc.ServiceDesc{
		ServiceName: ServiceName,
		HandlerType: (*any)(nil),
		Methods: []grpc.MethodDesc{{
			MethodName: MethodEcho,
			Handler: func(_ any, ctx context.Context, dec func(any) error, _ grpc.UnaryServerInterceptor) (any, error) {
				var req []byte
				if err := dec(&req); err != nil {
					return nil, err
				}
				resp, err := h(ctx, req)
				if err != nil {
					return nil, err
				}
				return &resp, nil
			},
		}},
		Streams:  []grpc.StreamDesc{},
		Metadata: "bench.raw",
	}
}
