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
	// The H3 transports own their own QUIC dialing, so they take TLSConfig
	// directly rather than a ConnOpts.Dialer.
	c, err := pclient.NewClient(pclient.ClientOptions{
		Addr:      Addr(cfg.Host, cfg.H3Port),
		Transport: pclient.TransportH3Pool,
		Pool: &pclient.PoolOptions{
			MaxConnsPerHost:   cfg.Conns,
			MaxStreamsPerConn: cfg.MaxStreamsPerConn,
		},
		TLSConfig: tlsConfig(cfg),
	})
	if err != nil {
		return nil, fmt.Errorf("poseidon h3: %w", err)
	}
	return &poseidonHTTP{c: c, host: Addr(cfg.Host, cfg.H3Port)}, nil
}

// poseidonGRPC drives poseidon's gRPC client. It holds a fixed set of
// connections and round-robins calls across them, matching how grpc-go's
// ClientConn multiplexes — neither arm gets a connection-count advantage.
type poseidonGRPC struct {
	conns []*pgrpc.ClientConn
	next  chan int
}

func newPoseidonGRPC(cfg Config) (Client, error) {
	addr := Addr(cfg.Host, cfg.GRPCPort)
	n := cfg.Conns
	if n < 1 {
		n = 1
	}
	p := &poseidonGRPC{next: make(chan int, n)}
	for i := 0; i < n; i++ {
		cc, err := pgrpc.Dial(context.Background(), addr, pgrpc.Options{
			Authority: addr,
			Scheme:    "https",
			Conn: pconn.ConnOptions{
				Dialer: &pconn.TLSDialer{Config: tlsConfig(cfg, "h2")},
			},
		})
		if err != nil {
			for _, c := range p.conns {
				_ = c.Close()
			}
			return nil, fmt.Errorf("poseidon grpc dial: %w", err)
		}
		p.conns = append(p.conns, cc)
		p.next <- i
	}
	return p, nil
}

func (p *poseidonGRPC) Do(ctx context.Context, call Call) (Result, error) {
	// Take a connection slot, use it, hand it back. Bounded checkout keeps
	// in-flight streams per connection comparable to the grpc-go arm.
	var i int
	select {
	case i = <-p.next:
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
	defer func() { p.next <- i }()

	resp, err := p.conns[i].Invoke(ctx, rawgrpc.FullMethodEcho, call.Body, nil)
	if err != nil {
		return Result{}, err
	}
	return Result{Status: 200, BodyLen: len(resp)}, nil
}

func (p *poseidonGRPC) Close() error {
	var firstErr error
	for _, c := range p.conns {
		if err := c.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
