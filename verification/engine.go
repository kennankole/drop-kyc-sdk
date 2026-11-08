package verification

import (
	"context"
	"errors"
	"time"
)

// Engine runs checks against registered providers. For each check it asks
// the providers that support it in registration order and returns the
// first authoritative answer; a provider that is unavailable hands over to
// the next one. When every provider is unavailable the check's Result is
// OutcomeUnavailable, never a pass.
//
// An Engine is safe for concurrent use once constructed.
type Engine struct {
	verifiers []Verifier
	observer  Observer
	now       func() time.Time
}

// EngineOption configures an Engine.
type EngineOption func(*Engine)

// WithObserver reports every provider call to o.
func WithObserver(o Observer) EngineOption {
	return func(e *Engine) {
		if o != nil {
			e.observer = o
		}
	}
}

// WithClock overrides the time source stamped on synthesized Results.
func WithClock(now func() time.Time) EngineOption {
	return func(e *Engine) {
		if now != nil {
			e.now = now
		}
	}
}

// NewEngine returns an Engine over verifiers, in priority order. Nil
// verifiers are ignored, so optional providers can be passed unconditionally.
func NewEngine(verifiers []Verifier, opts ...EngineOption) *Engine {
	e := &Engine{observer: NoopObserver, now: time.Now}

	for _, v := range verifiers {
		if v != nil {
			e.verifiers = append(e.verifiers, v)
		}
	}

	for _, opt := range opts {
		opt(e)
	}

	return e
}

// Supports reports whether any registered provider can run the check.
func (e *Engine) Supports(check CheckType) bool {
	for _, v := range e.verifiers {
		if v.Supports(check) {
			return true
		}
	}

	return false
}

// Run runs each requested check that some provider supports and returns
// one Result per such check, in request order. Checks no provider
// supports are skipped, so a caller can always ask for everything it would
// like to know and get answers for what is configured.
func (e *Engine) Run(ctx context.Context, subject *Subject, checks ...CheckType) []Result {
	results := make([]Result, 0, len(checks))

	for _, check := range checks {
		if r, ok := e.run(ctx, check, subject); ok {
			results = append(results, r)
		}
	}

	return results
}

func (e *Engine) run(ctx context.Context, check CheckType, subject *Subject) (Result, bool) {
	var (
		asked    bool
		lastErr  error
		fallback bool
	)

	for _, v := range e.verifiers {
		if !v.Supports(check) {
			continue
		}

		asked = true
		start := time.Now()
		r, err := v.Verify(ctx, check, subject)
		e.observer.Observe(&Event{
			Check: check, Provider: v.Provider(), Outcome: r.Outcome, Err: err,
			Latency: time.Since(start), Fallback: fallback,
		})

		if err == nil {
			normalize(&r, check, v.Provider(), e.now)

			return r, true
		}

		lastErr = err

		if errors.Is(err, ErrInvalidSubject) {
			return Result{
				Check: check, Provider: v.Provider(), Outcome: OutcomeInconclusive,
				Reason: err.Error(), CheckedAt: e.now().UTC(),
			}, true
		}

		// ErrUnavailable, ErrUnsupported, or anything else the provider
		// failed with: try the next provider.
		fallback = true
	}

	if !asked {
		return Result{}, false
	}

	return Result{
		Check: check, Outcome: OutcomeUnavailable,
		Reason: unavailableReason(lastErr), CheckedAt: e.now().UTC(),
	}, true
}

// normalize fills the fields a provider may leave empty, so every Result
// the Engine returns names its check, provider and time.
func normalize(r *Result, check CheckType, provider string, now func() time.Time) {
	r.Check = check
	if r.Provider == "" {
		r.Provider = provider
	}

	if r.Outcome == "" {
		r.Outcome = OutcomeInconclusive
	}

	if r.CheckedAt.IsZero() {
		r.CheckedAt = now()
	}

	r.CheckedAt = r.CheckedAt.UTC()
}

func unavailableReason(err error) string {
	if err == nil {
		return "no provider answered"
	}

	return "no provider answered: " + err.Error()
}
