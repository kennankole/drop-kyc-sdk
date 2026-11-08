package verification_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/kennankole/drop-kyc-sdk/verification"
)

type fakeVerifier struct {
	name   string
	checks []verification.CheckType
	result verification.Result
	err    error
	calls  int
}

func (f *fakeVerifier) Provider() string { return f.name }

func (f *fakeVerifier) Supports(c verification.CheckType) bool {
	for _, s := range f.checks {
		if s == c {
			return true
		}
	}

	return false
}

func (f *fakeVerifier) Verify(context.Context, verification.CheckType, *verification.Subject) (verification.Result, error) {
	f.calls++

	return f.result, f.err
}

var fixed = time.Date(2026, 11, 7, 12, 0, 0, 0, time.FixedZone("EAT", 3*3600))

func clock() time.Time { return fixed }

func TestEngine_FirstAuthoritativeAnswerWins(t *testing.T) {
	primary := &fakeVerifier{name: "a", checks: []verification.CheckType{verification.CheckTaxPIN},
		result: verification.Result{Outcome: verification.OutcomeFail, Reason: "not the owner"}}
	second := &fakeVerifier{name: "b", checks: []verification.CheckType{verification.CheckTaxPIN}}

	got := verification.NewEngine([]verification.Verifier{primary, second}, verification.WithClock(clock)).
		Run(context.Background(), &verification.Subject{}, verification.CheckTaxPIN)

	if len(got) != 1 || got[0].Outcome != verification.OutcomeFail || got[0].Provider != "a" {
		t.Fatalf("got %+v, want one fail from a", got)
	}

	if second.calls != 0 {
		t.Fatal("a negative answer is authoritative: no fallback")
	}

	if got[0].Check != verification.CheckTaxPIN || !got[0].CheckedAt.Equal(fixed) || got[0].CheckedAt.Location() != time.UTC {
		t.Fatalf("result not normalized: %+v", got[0])
	}
}

func TestEngine_FallsBackWhenUnavailable(t *testing.T) {
	var events []verification.Event

	down := &fakeVerifier{name: "a", checks: []verification.CheckType{verification.CheckNationalID},
		err: fmt.Errorf("timeout: %w", verification.ErrUnavailable)}
	up := &fakeVerifier{name: "b", checks: []verification.CheckType{verification.CheckNationalID},
		result: verification.Result{Outcome: verification.OutcomePass}}

	got := verification.NewEngine([]verification.Verifier{down, nil, up},
		verification.WithObserver(verification.ObserverFunc(func(e *verification.Event) { events = append(events, *e) }))).
		Run(context.Background(), &verification.Subject{}, verification.CheckNationalID)

	if len(got) != 1 || got[0].Outcome != verification.OutcomePass || got[0].Provider != "b" {
		t.Fatalf("got %+v, want pass from b", got)
	}

	if len(events) != 2 || events[0].Fallback || !events[1].Fallback || !errors.Is(events[0].Err, verification.ErrUnavailable) {
		t.Fatalf("events %+v", events)
	}
}

func TestEngine_AllUnavailableIsNeverAPass(t *testing.T) {
	down := &fakeVerifier{name: "a", checks: []verification.CheckType{verification.CheckTaxPIN},
		err: verification.ErrUnavailable}

	got := verification.NewEngine([]verification.Verifier{down}).
		Run(context.Background(), &verification.Subject{}, verification.CheckTaxPIN)

	if len(got) != 1 || got[0].Outcome != verification.OutcomeUnavailable || got[0].Provider != "" {
		t.Fatalf("got %+v", got)
	}
}

func TestEngine_InvalidSubjectDoesNotFallBack(t *testing.T) {
	a := &fakeVerifier{name: "a", checks: []verification.CheckType{verification.CheckTaxPIN},
		err: fmt.Errorf("no PIN: %w", verification.ErrInvalidSubject)}
	b := &fakeVerifier{name: "b", checks: []verification.CheckType{verification.CheckTaxPIN}}

	got := verification.NewEngine([]verification.Verifier{a, b}).
		Run(context.Background(), &verification.Subject{}, verification.CheckTaxPIN)

	if len(got) != 1 || got[0].Outcome != verification.OutcomeInconclusive || b.calls != 0 {
		t.Fatalf("got %+v, b.calls=%d", got, b.calls)
	}
}

func TestEngine_SkipsUnconfiguredChecks(t *testing.T) {
	kra := &fakeVerifier{name: "a", checks: []verification.CheckType{verification.CheckTaxPIN},
		result: verification.Result{Outcome: verification.OutcomePass, Provider: "a-sandbox"}}
	e := verification.NewEngine([]verification.Verifier{kra})

	got := e.Run(context.Background(), &verification.Subject{},
		verification.CheckNationalID, verification.CheckTaxPIN, verification.CheckSelfieMatch)

	if len(got) != 1 || got[0].Check != verification.CheckTaxPIN || got[0].Provider != "a-sandbox" {
		t.Fatalf("got %+v", got)
	}

	if e.Supports(verification.CheckSelfieMatch) || !e.Supports(verification.CheckTaxPIN) {
		t.Fatal("Supports disagrees with the registered providers")
	}

	if empty := verification.NewEngine(nil).Run(context.Background(), &verification.Subject{}, verification.CheckTaxPIN); len(empty) != 0 {
		t.Fatalf("no providers, no results: %+v", empty)
	}
}

func TestEngine_EmptyOutcomeIsInconclusive(t *testing.T) {
	v := &fakeVerifier{name: "a", checks: []verification.CheckType{verification.CheckTaxPIN}}

	got := verification.NewEngine([]verification.Verifier{v}).
		Run(context.Background(), &verification.Subject{}, verification.CheckTaxPIN)
	if got[0].Outcome != verification.OutcomeInconclusive || got[0].CheckedAt.IsZero() {
		t.Fatalf("got %+v", got[0])
	}
}
