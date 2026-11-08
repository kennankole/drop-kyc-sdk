package verification

import "time"

// Event describes one provider call made by the Engine.
type Event struct {
	Check    CheckType
	Provider string
	Outcome  Outcome // empty when Err is set
	Err      error
	Latency  time.Duration
	// Fallback is true when an earlier provider for this check was
	// unavailable.
	Fallback bool
}

// Observer receives an Event for every provider call, so applications can
// wire metrics and alarms without this package depending on a metrics
// library.
type Observer interface {
	Observe(*Event)
}

// ObserverFunc adapts a function to Observer.
type ObserverFunc func(*Event)

// Observe implements Observer.
func (f ObserverFunc) Observe(e *Event) { f(e) }

type noopObserver struct{}

func (noopObserver) Observe(*Event) {}

// NoopObserver discards every event.
var NoopObserver Observer = noopObserver{}
