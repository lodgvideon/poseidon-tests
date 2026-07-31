// Package clientset is the seam the whole benchmark turns on: one Client
// interface with eight implementations — poseidon and standard, for each of
// HTTP/1.1, HTTP/2, HTTP/3, and gRPC.
//
// The driver above this seam is identical for every arm. It generates the same
// payloads in the same order and issues the same calls at the same rate; the
// only thing that changes between two runs of a regime is which implementation
// of this interface is plugged in. That is what lets a resource delta be
// attributed to the client library. See ADR-0001.
package clientset

import (
	"context"
	"fmt"
	"strings"
)

// Regime is a protocol under test.
type Regime string

// The four regimes. gRPC is not an HTTP version but is included as a fourth
// regime because poseidon ships a first-class gRPC client (see CONTEXT.md).
const (
	RegimeH1   Regime = "h1"
	RegimeH2   Regime = "h2"
	RegimeH3   Regime = "h3"
	RegimeGRPC Regime = "grpc"
)

// Arm is which client library is under test.
type Arm string

const (
	// ArmPoseidon is poseidon-http-client.
	ArmPoseidon Arm = "poseidon"
	// ArmStandard is the incumbent a consumer would otherwise use:
	// net/http for H1/H2, quic-go/http3 for H3, grpc-go for gRPC.
	ArmStandard Arm = "standard"
	// ArmStandardPooled is a DIAGNOSTIC arm, deliberately outside the scored
	// matrix. It is byte-for-byte the standard arm except that the response
	// body is materialised into a sync.Pool'd bytes.Buffer instead of a fresh
	// io.ReadAll buffer per request.
	//
	// It exists to split the flagship H1/H2 bytes-per-request win into the part
	// that is transport efficiency and the part that is body-materialisation
	// idiom: poseidon's caller-owned reusable Response against net/http's
	// per-request io.ReadAll. Running it answers "what would a consumer who
	// already pools buffers around net/http actually gain?".
	//
	// It is NOT in Arms and NOT in report.py's ARMS, so the committed 4x2
	// matrix and the comparison table are unaffected.
	ArmStandardPooled Arm = "standard-pooled"
)

// Regimes and Arms enumerate the full 4x2 matrix. ArmStandardPooled is
// deliberately absent: it is a diagnostic, not a scored cell.
var (
	Regimes = []Regime{RegimeH1, RegimeH2, RegimeH3, RegimeGRPC}
	Arms    = []Arm{ArmPoseidon, ArmStandard}
)

// parseableArms is what ParseArm accepts — the scored matrix plus the
// diagnostic arm, which an operator can select explicitly but which no matrix
// enumeration will ever produce on its own.
var parseableArms = []Arm{ArmPoseidon, ArmStandard, ArmStandardPooled}

// Call is one request to issue. It is protocol-agnostic: each implementation
// maps it onto its own wire representation.
type Call struct {
	Scenario string
	Method   string
	Path     string
	// Body is the request body, or nil. The slice may alias a reusable
	// buffer owned by the caller and must not be retained past Do.
	Body []byte
}

// Result reports what came back. Status is the HTTP status (or the gRPC status
// mapped onto 200/500), BodyLen the number of response body bytes received.
type Result struct {
	Status  int
	BodyLen int
}

// Client issues calls for one regime with one library. Implementations must be
// safe for concurrent use by the driver's worker goroutines.
type Client interface {
	// Do issues one call. A non-2xx response is NOT an error — it is
	// reported via Result.Status. Only transport failures return err.
	Do(ctx context.Context, c Call) (Result, error)
	Close() error
}

// Config is everything an implementation needs to connect.
type Config struct {
	// Host is the target hostname (also the TLS ServerName and :authority).
	Host string
	// TLSPort serves the H1 and H2 legs via ALPN; H3Port speaks QUIC;
	// GRPCPort speaks gRPC over TLS.
	TLSPort, H3Port, GRPCPort int
	// Conns is the connection-pool size. For H1 this IS the request
	// concurrency (one exchange per connection); for H2/H3 connections
	// multiplex, so it is the number of transport connections.
	Conns int
	// MaxStreamsPerConn caps multiplexed streams on H2/H3 connections.
	MaxStreamsPerConn int
	// InsecureSkipVerify is always true here: the target serves a
	// self-signed certificate generated at startup.
	InsecureSkipVerify bool
}

// Addr renders "host:port".
func Addr(host string, port int) string {
	return fmt.Sprintf("%s:%d", host, port)
}

// New builds the client for one (regime, arm) cell of the matrix.
func New(regime Regime, arm Arm, cfg Config) (Client, error) {
	switch {
	case regime == RegimeH1 && arm == ArmPoseidon:
		return newPoseidonH1(cfg)
	case regime == RegimeH1 && arm == ArmStandard:
		return newStdH1(cfg)
	case regime == RegimeH2 && arm == ArmPoseidon:
		return newPoseidonH2(cfg)
	case regime == RegimeH2 && arm == ArmStandard:
		return newStdH2(cfg)
	case regime == RegimeH3 && arm == ArmPoseidon:
		return newPoseidonH3(cfg)
	case regime == RegimeH3 && arm == ArmStandard:
		return newQuicGoH3(cfg)
	case regime == RegimeGRPC && arm == ArmPoseidon:
		return newPoseidonGRPC(cfg)
	case regime == RegimeGRPC && arm == ArmStandard:
		return newGRPCGo(cfg)
	case regime == RegimeH1 && arm == ArmStandardPooled:
		return newStdH1Pooled(cfg)
	case regime == RegimeH2 && arm == ArmStandardPooled:
		return newStdH2Pooled(cfg)
	}
	return nil, fmt.Errorf("clientset: no implementation for regime=%q arm=%q", regime, arm)
}

// ParseRegime validates a regime name.
func ParseRegime(s string) (Regime, error) {
	for _, r := range Regimes {
		if string(r) == strings.ToLower(s) {
			return r, nil
		}
	}
	return "", fmt.Errorf("clientset: unknown regime %q (want one of h1, h2, h3, grpc)", s)
}

// ParseArm validates an arm name.
func ParseArm(s string) (Arm, error) {
	for _, a := range parseableArms {
		if string(a) == strings.ToLower(s) {
			return a, nil
		}
	}
	return "", fmt.Errorf("clientset: unknown arm %q (want poseidon, standard, or standard-pooled)", s)
}
