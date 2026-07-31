// Command target is the neutral benchmark target. It serves one chi handler
// over every transport under test, plus a gRPC listener carrying the same
// payload bytes:
//
//	:8080  cleartext HTTP/1.1 + h2c   (health checks and debugging only)
//	:8443  TLS, ALPN [h2, http/1.1]   (the HTTP/1.1 AND HTTP/2 legs)
//	:8444  HTTP/3 over QUIC           (the HTTP/3 leg)
//	:9443  gRPC over TLS              (the gRPC leg)
//
// Two deliberate choices:
//
// The H1 and H2 legs share ONE listener with one TLS config. The client picks
// the protocol by offering only "http/1.1" or only "h2" in ALPN, so the two
// legs differ in nothing but the protocol the client asked for.
//
// Every measured leg runs over TLS. QUIC mandates TLS 1.3, so making the other
// three cleartext would leave the H3 row paying a handshake cost the others
// dodge, and rows are supposed to be comparable to each other (ADR-0003).
package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/quic-go/quic-go/http3"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/lodgvideon/poseidon-tests/internal/api"
	"github.com/lodgvideon/poseidon-tests/internal/payload"
	"github.com/lodgvideon/poseidon-tests/internal/rawgrpc"
	"github.com/lodgvideon/poseidon-tests/internal/tlsutil"
)

func main() {
	var (
		plainAddr = flag.String("plain", ":8080", "cleartext HTTP/1.1 + h2c address (health/debug)")
		tlsAddr   = flag.String("tls", ":8443", "TLS address serving both the H1 and H2 legs via ALPN")
		h3Addr    = flag.String("h3", ":8444", "HTTP/3 (QUIC) address")
		grpcAddr  = flag.String("grpc", ":9443", "gRPC over TLS address")
		seed      = flag.Uint64("seed", 1, "deterministic seed for server-generated bodies")
	)
	flag.Parse()

	handler := api.Handler(*seed)

	cert, err := tlsutil.SelfSigned()
	if err != nil {
		log.Fatalf("target: generate cert: %v", err)
	}
	tlsCfg := func(protos ...string) *tls.Config {
		return &tls.Config{
			Certificates: []tls.Certificate{cert},
			NextProtos:   protos,
			MinVersion:   tls.VersionTLS12,
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// --- cleartext: health checks, kubectl probes, manual curl ------------
	plain := &http.Server{
		Addr:              *plainAddr,
		Handler:           h2c.NewHandler(handler, &http2.Server{}),
		ReadHeaderTimeout: 10 * time.Second,
	}

	// --- TLS: serves the H1 leg and the H2 leg, selected by client ALPN ---
	tlsSrv := &http.Server{
		Addr:              *tlsAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		TLSConfig:         tlsCfg("h2", "http/1.1"),
	}
	// Raise MaxConcurrentStreams well above what any client arm requests.
	// At the default 250 a client pooling 250 streams per connection races
	// the server's limit and takes occasional REFUSED_STREAM resets — a few
	// per ten thousand requests, enough to pollute the error column without
	// telling us anything about the client library.
	if err := http2.ConfigureServer(tlsSrv, &http2.Server{MaxConcurrentStreams: 1000}); err != nil {
		log.Fatalf("target: configure http2: %v", err)
	}

	// --- HTTP/3 over QUIC --------------------------------------------------
	h3Cfg := tlsCfg("h3")
	h3Cfg.MinVersion = tls.VersionTLS13
	h3Srv := &http3.Server{Addr: *h3Addr, Handler: handler, TLSConfig: h3Cfg}

	// --- gRPC --------------------------------------------------------------
	// Echo mirrors the HTTP /v1/echo route: decode the JSON body, hand the
	// items back. Same work and same bytes as the HTTP legs, different
	// transport — which is the only thing the comparison is allowed to vary.
	// ForceServerCodec pins the raw pass-through codec regardless of the
	// content-subtype the client advertises. grpc-go's client announces
	// "application/grpc+rawbytes" (it derives the subtype from the codec
	// name), but poseidon's gRPC client hardcodes a bare "application/grpc",
	// which the server would otherwise resolve to the default protobuf codec
	// and reject with "message is *[]uint8, want proto.Message".
	grpcSrv := grpc.NewServer(
		grpc.Creds(credentials.NewTLS(tlsCfg("h2"))),
		grpc.ForceServerCodec(rawgrpc.Codec{}),
	)
	grpcSrv.RegisterService(rawgrpc.ServiceDesc(func(_ context.Context, req []byte) ([]byte, error) {
		var in payload.Body
		if err := json.Unmarshal(req, &in); err != nil {
			return nil, err
		}
		return json.Marshal(payload.Body{Seq: in.Seq, Scenario: "echo", Items: in.Items})
	}), new(any))

	serve := func(name string, fn func() error) {
		go func() {
			log.Printf("target: %s listening", name)
			if err := fn(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Printf("target: %s stopped: %v", name, err)
			}
		}()
	}

	serve("cleartext "+*plainAddr, plain.ListenAndServe)
	serve("tls-alpn(h2,http/1.1) "+*tlsAddr, func() error { return tlsSrv.ListenAndServeTLS("", "") })
	serve("h3 "+*h3Addr, h3Srv.ListenAndServe)
	serve("grpc-tls "+*grpcAddr, func() error {
		ln, err := net.Listen("tcp", *grpcAddr)
		if err != nil {
			return err
		}
		return grpcSrv.Serve(ln)
	})

	<-ctx.Done()
	log.Print("target: draining")

	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = plain.Shutdown(shutCtx)
	_ = tlsSrv.Shutdown(shutCtx)
	_ = h3Srv.Close()
	grpcSrv.GracefulStop()
}
