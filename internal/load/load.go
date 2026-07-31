// Package load implements the closed-loop rate model: a linear ramp to the
// target rate, then a plateau held for the measurement window.
//
// Closed-loop and rate-limited is the point (ADR-0002). Both arms deliver the
// same request rate, so a difference in CPU or allocations is a difference in
// cost-per-request, not a difference in how much work was done.
package load

import (
	"context"
	"time"
)

// Profile describes the shape of a run.
type Profile struct {
	// TargetRPS is the plateau rate, shared by every arm.
	TargetRPS float64
	// Ramp is how long to climb linearly from ~0 to TargetRPS.
	Ramp time.Duration
	// Plateau is how long to hold TargetRPS. Measurements are taken from
	// this window only, so that GC and pool warm-up during the ramp do not
	// pollute the numbers.
	Plateau time.Duration
}

// Total is the full run duration.
func (p Profile) Total() time.Duration { return p.Ramp + p.Plateau }

// rateAt returns the target rate at elapsed time t.
func (p Profile) rateAt(t time.Duration) float64 {
	if t >= p.Ramp {
		return p.TargetRPS
	}
	if p.Ramp <= 0 {
		return p.TargetRPS
	}
	frac := float64(t) / float64(p.Ramp)
	// Start at a small non-zero fraction so the first ticks are not
	// separated by a near-infinite interval.
	const floor = 0.02
	if frac < floor {
		frac = floor
	}
	return p.TargetRPS * frac
}

// Ticker paces request issuance. Workers call Wait to be released for one
// request; the pacing is global, so the aggregate rate across all workers is
// the profile rate regardless of worker count.
type Ticker struct {
	profile Profile
	start   time.Time
	permits chan struct{}
	done    chan struct{}
}

// NewTicker starts pacing immediately and returns once the generator is
// running. Callers must call Stop.
func NewTicker(profile Profile) *Ticker {
	t := &Ticker{
		profile: profile,
		start:   time.Now(),
		// A small buffer absorbs brief scheduling jitter without letting
		// the generator run far ahead and turn the run open-loop.
		permits: make(chan struct{}, 64),
		done:    make(chan struct{}),
	}
	go t.run()
	return t
}

func (t *Ticker) run() {
	defer close(t.permits)
	// Re-evaluate the target rate on a coarse cadence; within a slice the
	// interval is fixed. 100ms is fine granularity against a 5-minute ramp.
	const slice = 100 * time.Millisecond
	for {
		select {
		case <-t.done:
			return
		default:
		}

		elapsed := time.Since(t.start)
		if elapsed >= t.profile.Total() {
			return
		}

		rate := t.profile.rateAt(elapsed)
		if rate <= 0 {
			time.Sleep(slice)
			continue
		}

		n := int(rate * slice.Seconds())
		if n < 1 {
			n = 1
		}
		interval := time.Duration(float64(slice) / float64(n))

		sliceEnd := time.Now().Add(slice)
		for i := 0; i < n; i++ {
			select {
			case t.permits <- struct{}{}:
			case <-t.done:
				return
			}
			if d := time.Until(sliceEnd.Add(-time.Duration(n-i-1) * interval)); d > 0 {
				time.Sleep(d)
			}
		}
	}
}

// Wait blocks until this worker may issue one request. It returns false when
// the run is over or ctx is cancelled.
func (t *Ticker) Wait(ctx context.Context) bool {
	select {
	case _, ok := <-t.permits:
		return ok
	case <-ctx.Done():
		return false
	}
}

// PlateauStart is the instant the plateau begins.
func (t *Ticker) PlateauStart() time.Time { return t.start.Add(t.profile.Ramp) }

// Stop halts pacing.
func (t *Ticker) Stop() { close(t.done) }
