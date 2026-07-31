// Command driver is the process under measurement. It links exactly one
// client library, drives the shared scenario mix at a fixed rate, and reports
// its own CPU, memory, and allocation counts.
//
// Everything above the clientset seam is identical between the two arms of a
// regime: same payload sequence (same seed), same scenario order, same rate.
// The only variable is which client implementation was selected, which is what
// makes a delta attributable to the library. See ADR-0001.
//
// Measurements are taken from the plateau window only. Ramp-phase numbers
// include connection establishment, TLS handshakes, and GC warm-up, none of
// which represent steady-state cost.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	httppprof "net/http/pprof"
	"os"
	"os/signal"
	"runtime"
	"runtime/pprof"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/lodgvideon/poseidon-tests/internal/clientset"
	"github.com/lodgvideon/poseidon-tests/internal/load"
	"github.com/lodgvideon/poseidon-tests/internal/payload"
	"github.com/lodgvideon/poseidon-tests/internal/scenario"
	"github.com/lodgvideon/poseidon-tests/internal/selfmetrics"
)

// counters are the driver's own request tallies. Split from selfmetrics
// because they are about work delivered, not resources consumed.
type counters struct {
	total    atomic.Uint64
	errs     atomic.Uint64
	non2xx   atomic.Uint64
	bodyByte atomic.Uint64

	// Latched when the plateau begins. Every reported figure is a delta
	// across the plateau, so each counter needs its own baseline — reporting
	// a plateau request count against a since-process-start error count
	// produces nonsense like "294 errors out of 250 requests".
	plateauStartTotal  atomic.Uint64
	plateauStartErrs   atomic.Uint64
	plateauStartNon2xx atomic.Uint64

	// firstErr keeps one example failure so a run that silently degrades
	// into all-errors is diagnosable from the report alone.
	errOnce  sync.Once
	firstErr atomic.Value // string
}

// Report is the run's machine-readable output, consumed by the table generator.
type Report struct {
	Regime  string `json:"regime"`
	Arm     string `json:"arm"`
	Profile struct {
		TargetRPS float64 `json:"target_rps"`
		RampSec   float64 `json:"ramp_seconds"`
		PlateauSec float64 `json:"plateau_seconds"`
	} `json:"profile"`

	// Plateau-window results — the only numbers the comparison table uses.
	PlateauRequests uint64  `json:"plateau_requests"`
	PlateauErrors   uint64  `json:"plateau_errors"`
	PlateauNon2xx   uint64  `json:"plateau_non_2xx"`
	AchievedRPS     float64 `json:"achieved_rps"`
	// SampleError is one example transport failure, empty on a clean run.
	SampleError string `json:"sample_error,omitempty"`

	CPUBusySeconds float64 `json:"cpu_busy_seconds"`
	CPUGCSeconds   float64 `json:"cpu_gc_seconds"`
	CPUMillicores  float64 `json:"cpu_millicores"`

	AllocBytesTotal   uint64  `json:"alloc_bytes_total"`
	AllocObjectsTotal uint64  `json:"alloc_objects_total"`
	AllocBytesPerReq  float64 `json:"alloc_bytes_per_req"`
	AllocsPerReq      float64 `json:"allocs_per_req"`

	RSSAvgBytes  uint64 `json:"rss_avg_bytes"`
	RSSPeakBytes uint64 `json:"rss_peak_bytes"`
	HeapAvgBytes uint64 `json:"heap_avg_bytes"`

	Start selfmetrics.Snapshot `json:"plateau_start_snapshot"`
	End   selfmetrics.Snapshot `json:"plateau_end_snapshot"`
}

func main() {
	var (
		regimeFlag = flag.String("regime", "h2", "protocol regime: h1, h2, h3, grpc")
		armFlag    = flag.String("arm", "poseidon", "client under test: poseidon or standard")
		host       = flag.String("host", "target", "target hostname (also TLS ServerName)")
		tlsPort    = flag.Int("tls-port", 8443, "target TLS port (H1 and H2 legs)")
		h3Port     = flag.Int("h3-port", 8444, "target QUIC port (H3 leg)")
		grpcPort   = flag.Int("grpc-port", 9443, "target gRPC port")
		rps        = flag.Float64("rps", 200, "plateau request rate")
		ramp       = flag.Duration("ramp", 5*time.Minute, "ramp duration")
		plateau    = flag.Duration("plateau", 20*time.Minute, "plateau duration (the measurement window)")
		workers    = flag.Int("workers", 64, "concurrent worker goroutines")
		conns      = flag.Int("conns", 8, "transport connections (for H1, also the request concurrency)")
		streams    = flag.Int("streams", 250, "max multiplexed streams per connection (H2/H3)")
		seed       = flag.Uint64("seed", 1, "payload+scenario seed; must match across arms")
		poolSize   = flag.Int("payload-pool", payload.DefaultPoolSize, "distinct payloads precomputed and shared by all workers")
		metricsAddr = flag.String("metrics-addr", ":9100", "Prometheus + pprof listen address")
		outPath    = flag.String("out", "", "write the JSON report here (default: stdout only)")
		profileDir = flag.String("profile-dir", "", "if set, write pprof heap+cpu snapshots here")
	)
	flag.Parse()

	regime, err := clientset.ParseRegime(*regimeFlag)
	if err != nil {
		log.Fatalf("driver: %v", err)
	}
	arm, err := clientset.ParseArm(*armFlag)
	if err != nil {
		log.Fatalf("driver: %v", err)
	}

	cfg := clientset.Config{
		Host:               *host,
		TLSPort:            *tlsPort,
		H3Port:             *h3Port,
		GRPCPort:           *grpcPort,
		Conns:              *conns,
		MaxStreamsPerConn:  *streams,
		InsecureSkipVerify: true,
	}

	client, err := clientset.New(regime, arm, cfg)
	if err != nil {
		log.Fatalf("driver: build client: %v", err)
	}
	defer func() { _ = client.Close() }()

	var cs counters
	profile := load.Profile{TargetRPS: *rps, Ramp: *ramp, Plateau: *plateau}

	// Expose metrics and pprof for the whole run, so Grafana can plot the
	// ramp as well as the plateau even though only the plateau is scored.
	go serveMetrics(*metricsAddr, string(regime), string(arm), &cs)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Printf("driver: regime=%s arm=%s target=%s rps=%.0f ramp=%s plateau=%s workers=%d conns=%d",
		regime, arm, *host, *rps, *ramp, *plateau, *workers, *conns)

	// One pool shared by every worker: memory is then independent of worker
	// count, and the RSS column measures the client rather than the fixture.
	pool, err := payload.NewPool(*seed, 0, nil, *poolSize)
	if err != nil {
		log.Fatalf("driver: build payload pool: %v", err)
	}

	ticker := load.NewTicker(profile)
	defer ticker.Stop()

	// Sample RSS/heap continuously so the report can give an average across
	// the plateau rather than a single end-of-run reading, which would be at
	// the mercy of where the GC cycle happened to land.
	sampler := newResidentSampler()
	go sampler.run(ctx, ticker.PlateauStart())

	var wg sync.WaitGroup
	wg.Add(*workers)
	for i := 0; i < *workers; i++ {
		go func(id int) {
			defer wg.Done()
			runWorker(ctx, id, *seed, pool, regime, client, ticker, &cs)
		}(i)
	}

	// Latch the plateau boundary: snapshot resources and request count the
	// instant the ramp ends, and again when the run finishes. Everything the
	// comparison table reports is the difference between these two points.
	plateauStart := waitUntil(ctx, ticker.PlateauStart())
	startSnap := selfmetrics.Read()
	cs.plateauStartTotal.Store(cs.total.Load())
	cs.plateauStartErrs.Store(cs.errs.Load())
	cs.plateauStartNon2xx.Store(cs.non2xx.Load())
	if plateauStart {
		log.Printf("driver: plateau reached — measurement window open")
		writeProfile(*profileDir, string(regime), string(arm), "start")
	}

	wg.Wait()
	endSnap := selfmetrics.Read()
	writeProfile(*profileDir, string(regime), string(arm), "end")

	rep := buildReport(regime, arm, profile, &cs, startSnap, endSnap, sampler)
	emit(rep, *outPath)
}

// runWorker issues calls until the run ends. Each worker has its own payload
// generator and scenario mix, both seeded from (seed, worker id), so the
// sequence is deterministic and reproducible across arms without any
// cross-worker synchronisation.
func runWorker(ctx context.Context, id int, seed uint64, pool *payload.Pool, regime clientset.Regime, c clientset.Client, t *load.Ticker, cs *counters) {
	mix := scenario.NewMix(seed, id, scenario.MixFor(regime))
	var seq uint64

	for t.Wait(ctx) {
		sc := mix.Next()
		seq++

		call := clientset.Call{
			Scenario: sc.Name,
			Method:   sc.Method,
			Path:     sc.Path,
		}
		if sc.HasBody {
			call.Body = pool.For(id, seq)
		}

		res, err := c.Do(ctx, call)
		cs.total.Add(1)
		switch {
		case err != nil:
			if ctx.Err() != nil {
				return
			}
			cs.errs.Add(1)
			cs.errOnce.Do(func() { cs.firstErr.Store(sc.Name + ": " + err.Error()) })
		default:
			if res.Status < 200 || res.Status >= 300 {
				cs.non2xx.Add(1)
			}
			cs.bodyByte.Add(uint64(res.BodyLen))
		}
	}
}

// residentSampler tracks RSS and heap over the plateau, since both oscillate
// with the GC cycle and a single reading would be arbitrary.
type residentSampler struct {
	mu       sync.Mutex
	rssSum   uint64
	heapSum  uint64
	n        uint64
	rssPeak  uint64
}

func newResidentSampler() *residentSampler { return &residentSampler{} }

func (s *residentSampler) run(ctx context.Context, plateauStart time.Time) {
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if time.Now().Before(plateauStart) {
				continue // ramp-phase readings are not part of the measurement
			}
			snap := selfmetrics.Read()
			mem := snap.RSSBytes
			if mem == 0 {
				mem = snap.TotalMemBytes // non-Linux fallback
			}
			s.mu.Lock()
			s.rssSum += mem
			s.heapSum += snap.HeapObjectsBytes
			s.n++
			if mem > s.rssPeak {
				s.rssPeak = mem
			}
			s.mu.Unlock()
		}
	}
}

func (s *residentSampler) result() (rssAvg, rssPeak, heapAvg uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.n == 0 {
		return 0, s.rssPeak, 0
	}
	return s.rssSum / s.n, s.rssPeak, s.heapSum / s.n
}

func buildReport(
	regime clientset.Regime, arm clientset.Arm, profile load.Profile,
	cs *counters, start, end selfmetrics.Snapshot, sampler *residentSampler,
) Report {
	d := selfmetrics.Sub(start, end)
	reqs := cs.total.Load() - cs.plateauStartTotal.Load()

	var rep Report
	rep.Regime = string(regime)
	rep.Arm = string(arm)
	rep.Profile.TargetRPS = profile.TargetRPS
	rep.Profile.RampSec = profile.Ramp.Seconds()
	rep.Profile.PlateauSec = profile.Plateau.Seconds()

	rep.PlateauRequests = reqs
	rep.PlateauErrors = cs.errs.Load() - cs.plateauStartErrs.Load()
	rep.PlateauNon2xx = cs.non2xx.Load() - cs.plateauStartNon2xx.Load()
	if s, ok := cs.firstErr.Load().(string); ok {
		rep.SampleError = s
	}
	rep.CPUBusySeconds = d.CPUBusySeconds
	rep.CPUGCSeconds = d.CPUGCSeconds
	rep.AllocBytesTotal = d.AllocBytes
	rep.AllocObjectsTotal = d.AllocObjects
	rep.Start = start
	rep.End = end

	if secs := d.Duration.Seconds(); secs > 0 {
		rep.AchievedRPS = float64(reqs) / secs
		// Millicores is the unit k8s requests/limits are written in, so the
		// number can be read directly against a pod spec.
		rep.CPUMillicores = d.CPUBusySeconds / secs * 1000
	}
	if reqs > 0 {
		rep.AllocBytesPerReq = float64(d.AllocBytes) / float64(reqs)
		rep.AllocsPerReq = float64(d.AllocObjects) / float64(reqs)
	}
	rep.RSSAvgBytes, rep.RSSPeakBytes, rep.HeapAvgBytes = sampler.result()
	return rep
}

// ReportMarker prefixes the machine-readable report line.
//
// Kubernetes merges a container's stdout and stderr into one log stream, so a
// pretty-printed multi-line JSON document on stdout can be sliced apart by a
// log line arriving on stderr between two of its lines — which is exactly what
// happened, corrupting one cell of a matrix run. The report is therefore
// emitted as a single marker-prefixed line: one Println is one write, so
// nothing can land inside it, and extraction is an unambiguous grep rather
// than a "from { to }" range match.
const ReportMarker = "POSEIDON_REPORT_JSON "

func emit(rep Report, outPath string) {
	pretty, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		log.Fatalf("driver: marshal report: %v", err)
	}
	compact, err := json.Marshal(rep)
	if err != nil {
		log.Fatalf("driver: marshal report: %v", err)
	}

	if outPath != "" {
		if err := os.WriteFile(outPath, pretty, 0o644); err != nil {
			log.Printf("driver: write report to %s: %v", outPath, err)
		}
	}

	// Human summary first, machine line last, so the marker line is the final
	// thing written and cannot be followed by interleaved output.
	log.Printf("driver: done — %d requests in plateau, %.1f rps, %.1f allocs/req, %.0f millicores",
		rep.PlateauRequests, rep.AchievedRPS, rep.AllocsPerReq, rep.CPUMillicores)
	fmt.Println(ReportMarker + string(compact))
}

func serveMetrics(addr, regime, arm string, cs *counters) {
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		selfmetrics.WritePrometheus(w, regime, arm, selfmetrics.Read(), cs.total.Load(), cs.errs.Load())
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	// pprof gives the allocation attribution that counters alone cannot:
	// which code path is doing the allocating.
	mux.HandleFunc("/debug/pprof/", httppprof.Index)
	mux.HandleFunc("/debug/pprof/profile", httppprof.Profile)
	mux.Handle("/debug/pprof/heap", httppprof.Handler("heap"))
	mux.Handle("/debug/pprof/allocs", httppprof.Handler("allocs"))

	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	if err := srv.ListenAndServe(); err != nil {
		log.Printf("driver: metrics server: %v", err)
	}
}

// writeProfile captures a heap profile at a plateau boundary. Diffing the two
// with `go tool pprof -base` shows exactly what allocated during the window.
func writeProfile(dir, regime, arm, when string) {
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("driver: profile dir: %v", err)
		return
	}
	name := fmt.Sprintf("%s/%s-%s-heap-%s.pprof", dir, regime, arm, when)
	f, err := os.Create(name) //nolint:gosec // path is operator-supplied
	if err != nil {
		log.Printf("driver: create profile: %v", err)
		return
	}
	defer func() { _ = f.Close() }()
	runtime.GC() // a fresh GC makes the live-heap numbers meaningful
	if err := pprof.WriteHeapProfile(f); err != nil {
		log.Printf("driver: write heap profile: %v", err)
		return
	}
	log.Printf("driver: heap profile written to %s", name)
}

// waitUntil sleeps until t, reporting false if ctx ended first.
func waitUntil(ctx context.Context, t time.Time) bool {
	d := time.Until(t)
	if d <= 0 {
		return true
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}
