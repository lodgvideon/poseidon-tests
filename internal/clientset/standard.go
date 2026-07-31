package clientset

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"

	"github.com/quic-go/quic-go/http3"
	"golang.org/x/net/http2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/lodgvideon/poseidon-tests/internal/rawgrpc"
)

// stdHTTP adapts any http.RoundTripper to the Client interface. The H1, H2 and
// H3 standard arms all use it, differing only in the transport underneath —
// mirroring how the poseidon arms share one adapter.
type stdHTTP struct {
	c    *http.Client
	base string
}

func (s *stdHTTP) Do(ctx context.Context, call Call) (Result, error) {
	var body io.Reader
	if call.Body != nil {
		// bytes.NewReader lets the transport set Content-Length and, for
		// retries, rewind — the same information poseidon gets from its
		// []byte body field.
		body = bytes.NewReader(call.Body)
	}
	req, err := http.NewRequestWithContext(ctx, call.Method, s.base+call.Path, body)
	if err != nil {
		return Result{}, err
	}
	if call.Body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := s.c.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	// Materialise the body, do not discard it.
	//
	// This matters more than it looks. The poseidon arm uses BodyBuffer,
	// which accumulates the whole body into the caller-owned Response — so
	// discarding here (io.Copy to io.Discard) would have this arm doing
	// strictly less work, and the bytes-per-request column would be
	// measuring "buffered vs. thrown away" rather than the two libraries.
	// In the first cluster run that gap read as poseidon allocating 118-173%
	// more, which was an artefact of this line.
	//
	// io.ReadAll is the idiomatic net/http body read, just as a reused
	// Response is the idiomatic poseidon one. Comparing each library as its
	// own documentation tells you to use it is the honest comparison; that
	// poseidon's reusable Response avoids a per-request allocation here is a
	// real design difference and is exactly what the number should show.
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return Result{}, err
	}
	return Result{Status: resp.StatusCode, BodyLen: len(respBody)}, nil
}

func (s *stdHTTP) Close() error {
	s.c.CloseIdleConnections()
	return nil
}

func newStdH1(cfg Config) (Client, error) {
	// ForceAttemptHTTP2 off and ALPN offering only http/1.1 pins this to
	// HTTP/1.1 on the target's shared TLS listener. MaxConnsPerHost is the
	// concurrency knob, matching TransportH1Pool's exclusive checkout.
	tr := &http.Transport{
		TLSClientConfig:     tlsConfig(cfg, "http/1.1"),
		ForceAttemptHTTP2:   false,
		MaxConnsPerHost:     cfg.Conns,
		MaxIdleConnsPerHost: cfg.Conns,
		DisableCompression:  true,
	}
	return &stdHTTP{
		c:    &http.Client{Transport: tr},
		base: "https://" + Addr(cfg.Host, cfg.TLSPort),
	}, nil
}

func newStdH2(cfg Config) (Client, error) {
	// x/net/http2.Transport is the idiomatic explicit-HTTP/2 client.
	// StrictMaxConcurrentStreams keeps stream concurrency per connection
	// bounded the way poseidon's MaxStreamsPerConn does, instead of silently
	// opening extra connections under load.
	tr := &http2.Transport{
		TLSClientConfig:            tlsConfig(cfg, "h2"),
		StrictMaxConcurrentStreams: true,
		DisableCompression:         true,
	}
	return &stdHTTP{
		c:    &http.Client{Transport: tr},
		base: "https://" + Addr(cfg.Host, cfg.TLSPort),
	}, nil
}

func newQuicGoH3(cfg Config) (Client, error) {
	// No stream-limit knob is set here. quic-go's MaxIncomingStreams bounds
	// PEER-initiated streams (quic-go v0.61.0 interface.go:152), not the
	// requests this client originates, so setting it to mirror poseidon's
	// MaxStreamsPerConn was a no-op dressed up as a matched limit.
	tr := &http3.Transport{
		TLSClientConfig:    tlsConfig(cfg),
		DisableCompression: true,
	}
	return &stdHTTP{
		c:    &http.Client{Transport: tr},
		base: "https://" + Addr(cfg.Host, cfg.H3Port),
	}, nil
}

// grpcGo drives google.golang.org/grpc — the incumbent a consumer would be
// migrating away from.
type grpcGo struct {
	cc *grpc.ClientConn
}

func newGRPCGo(cfg Config) (Client, error) {
	addr := Addr(cfg.Host, cfg.GRPCPort)
	tlsCfg := tlsConfig(cfg, "h2")
	cc, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)),
		// The raw codec carries the same JSON bytes as every other regime,
		// so no protobuf enters the comparison. See ADR-0003.
		grpc.WithDefaultCallOptions(grpc.ForceCodec(rawgrpc.Codec{})),
		grpc.WithContextDialer(func(ctx context.Context, a string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "tcp", a)
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("grpc-go dial: %w", err)
	}
	return &grpcGo{cc: cc}, nil
}

func (g *grpcGo) Do(ctx context.Context, call Call) (Result, error) {
	var resp []byte
	req := call.Body
	if err := g.cc.Invoke(ctx, rawgrpc.FullMethodEcho, &req, &resp); err != nil {
		return Result{}, err
	}
	return Result{Status: 200, BodyLen: len(resp)}, nil
}

func (g *grpcGo) Close() error { return g.cc.Close() }
