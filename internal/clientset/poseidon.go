package clientset

import (
	"context"
	"crypto/tls"
	"fmt"
	"sync"

	pclient "github.com/lodgvideon/poseidon-http-client/client"
	pconn "github.com/lodgvideon/poseidon-http-client/conn"
	// Aliased because the import path collides with google.golang.org/grpc,
	// which the standard gRPC arm imports. The poseidon grpc package's own
	// doc comment calls this out.
	pgrpc "github.com/lodgvideon/poseidon-http-client/grpc"

	"github.com/lodgvideon/poseidon-tests/internal/rawgrpc"
)

// poseidonHTTP adapts *pclient.Client to the Client interface. All three HTTP
// regimes share it — they differ only in how the underlying client is built.
type poseidonHTTP struct {
	c    *pclient.Client
	host string

	// Response objects are caller-owned and reusable in this API, which is
	// one of the things the library is designed around. Pooling them is the
	// idiomatic usage, so that is what we measure.
	respPool sync.Pool
}

func (p *poseidonHTTP) Do(ctx context.Context, call Call) (Result, error) {
	resp, _ := p.respPool.Get().(*pclient.Response)
	if resp == nil {
		resp = &pclient.Response{}
	}
	defer func() {
		resp.Reset()
		p.respPool.Put(resp)
	}()

	req := &pclient.Request{
		Method:    call.Method,
		Scheme:    "https",
		Authority: p.host,
		Path:      call.Path,
		Body:      call.Body,
		BodyMode:  pclient.BodyBuffer,
	}
	if err := p.c.Do(ctx, req, resp); err != nil {
		return Result{}, err
	}
	return Result{Status: resp.Status, BodyLen: len(resp.Body)}, nil
}

func (p *poseidonHTTP) Close() error { return p.c.Close() }

// tlsConfig builds the client TLS config, offering exactly the protocols the
// regime is supposed to use. Offering only "http/1.1" or only "h2" is how the
// H1 and H2 legs are separated on the target's single shared TLS listener.
func tlsConfig(cfg Config, protos ...string) *tls.Config {
	return &tls.Config{
		ServerName:         cfg.Host,
		NextProtos:         protos,
		InsecureSkipVerify: cfg.InsecureSkipVerify, //nolint:gosec // self-signed target cert
		MinVersion:         tls.VersionTLS12,
	}
}

func newPoseidonH1(cfg Config) (Client, error) {
	// conn.H1TLSDialer offers only "http/1.1" in ALPN and rejects a config
	// that asks for h2, rather than silently overriding it.
	//
	// It did not exist when this harness was written. conn.TLSDialer *prepended*
	// "h2" to whatever NextProtos it was given and then required the server to
	// pick h2, so the H1 leg silently ran over an h2-negotiated connection and
	// failed 100% of requests while still producing plausible-looking numbers.
	// That was reported as lodgvideon/poseidon-http-client#334 and fixed; this
	// arm now uses the upstream dialer instead of a local workaround.
	//
	// NextProtos is left unset: H1TLSDialer fills in ["http/1.1"] itself, and
	// passing it explicitly only risks tripping its ErrALPNConflict guard.
	tc := tlsConfig(cfg)
	tc.NextProtos = nil

	// TransportH1Pool is an exclusive-checkout pool: HTTP/1.1 carries one
	// exchange per connection, so MaxConnsPerHost IS the request concurrency.
	c, err := pclient.NewClient(pclient.ClientOptions{
		Addr:      Addr(cfg.Host, cfg.TLSPort),
		Transport: pclient.TransportH1Pool,
		Pool: &pclient.PoolOptions{
			MaxConnsPerHost: cfg.Conns,
		},
		ConnOpts: pconn.ConnOptions{
			Dialer: &pconn.H1TLSDialer{Config: tc},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("poseidon h1: %w", err)
	}
	return &poseidonHTTP{c: c, host: Addr(cfg.Host, cfg.TLSPort)}, nil
}

func newPoseidonH2(cfg Config) (Client, error) {
	c, err := pclient.NewClient(pclient.ClientOptions{
		Addr:      Addr(cfg.Host, cfg.TLSPort),
		Transport: pclient.TransportPool,
		Pool: &pclient.PoolOptions{
			MaxConnsPerHost:   cfg.Conns,
			MaxStreamsPerConn: cfg.MaxStreamsPerConn,
		},
		ConnOpts: pconn.ConnOptions{
			Dialer: &pconn.TLSDialer{Config: tlsConfig(cfg, "h2")},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("poseidon h2: %w", err)
	}
	return &poseidonHTTP{c: c, host: Addr(cfg.Host, cfg.TLSPort)}, nil
}

func newPoseidonH3(cfg Config) (Client, error) {
	// ONE QUIC connection, to match quic-go's http3.Transport, which has
	// exactly one connection per host by construction.
	//
	// This is the third instance of the same confound: an H3 pool sized to
	// cfg.Conns (8) was being compared against an architecturally-single
	// connection, exactly as the gRPC arm was before it was corrected, and as
	// the H1 arm would have been had its pool not happened to match. A
	// multi-connection arm pays per-connection state (crypto, ACK trackers,
	// congestion control) that the single-connection arm does not, which lands
	// in both the count and RSS columns.
	//
	// QUIC multiplexes streams within a connection, so one connection carries
	// the offered concurrency without serialising anything.
	//
	// The H3 transports own their own QUIC dialing, so they take TLSConfig
	// directly rather than a ConnOpts.Dialer.
	c, err := pclient.NewClient(pclient.ClientOptions{
		Addr:      Addr(cfg.Host, cfg.H3Port),
		Transport: pclient.TransportH3Pool,
		Pool: &pclient.PoolOptions{
			MaxConnsPerHost:   1,
			MaxStreamsPerConn: cfg.MaxStreamsPerConn,
		},
		TLSConfig: tlsConfig(cfg),
	})
	if err != nil {
		return nil, fmt.Errorf("poseidon h3: %w", err)
	}
	return &poseidonHTTP{c: c, host: Addr(cfg.Host, cfg.H3Port)}, nil
}

// poseidonGRPC drives poseidon's gRPC client over ONE connection, multiplexing
// concurrent Invokes across it — matching grpc-go's ClientConn, which is a
// single multiplexed connection and has no pool to configure.
//
// An earlier version dialled cfg.Conns (8) connections and checked one out per
// call, which serialised each connection to a single in-flight RPC. Its comment
// claimed parity with grpc-go; `netstat` during a run showed 8 established
// connections for this arm against grpc-go's 1, so the claim was written from
// intent and never verified. That topology put 8× the per-connection state
// (TLS, HPACK tables, framer buffers) into RSS — gRPC was the only regime where
// poseidon lost RSS — and changed write-coalescing behaviour, confounding CPU.
// Per-RPC allocation figures were unaffected, since both dominant sites are
// per-request regardless of connection count.
//
// poseidon's ClientConn multiplexes as many concurrent streams as the peer's
// SETTINGS_MAX_CONCURRENT_STREAMS allows, so the checkout was never needed.
type poseidonGRPC struct {
	cc *pgrpc.ClientConn
}

func newPoseidonGRPC(cfg Config) (Client, error) {
	addr := Addr(cfg.Host, cfg.GRPCPort)
	cc, err := pgrpc.Dial(context.Background(), addr, pgrpc.Options{
		Authority: addr,
		Scheme:    "https",
		Conn: pconn.ConnOptions{
			Dialer: &pconn.TLSDialer{Config: tlsConfig(cfg, "h2")},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("poseidon grpc dial: %w", err)
	}
	return &poseidonGRPC{cc: cc}, nil
}

func (p *poseidonGRPC) Do(ctx context.Context, call Call) (Result, error) {
	resp, err := p.cc.Invoke(ctx, rawgrpc.FullMethodEcho, call.Body, nil)
	if err != nil {
		return Result{}, err
	}
	return Result{Status: 200, BodyLen: len(resp)}, nil
}

func (p *poseidonGRPC) Close() error { return p.cc.Close() }
