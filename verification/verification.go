// Package verification defines vendor-neutral ports for identity and
// business verification (KYC): who is being checked (Subject), what is
// being asked (CheckType), and what the answer was (Result). It also
// provides an Engine that runs checks against registered providers in
// priority order, falling back to the next provider when one is
// unavailable.
//
// Application code should depend only on the types in this package, never
// on a provider package (such as verification/kra) outside of the
// composition root that constructs it. That is what lets a provider be
// added, reordered or replaced without touching business logic.
package verification

import (
	"context"
	"time"
)

// CheckType is a question a provider can answer about a Subject.
type CheckType string

const (
	// CheckNationalID: the national ID exists in the population register
	// and the name (and date of birth, when given) match it.
	CheckNationalID CheckType = "national_id"
	// CheckSelfieMatch: the selfie is a live person and matches the photo
	// held for the national ID.
	CheckSelfieMatch CheckType = "selfie_match"
	// CheckTaxPIN: the tax PIN belongs to the subject.
	CheckTaxPIN CheckType = "tax_pin"
	// CheckBusinessRegistry: the business is registered and the subject is
	// one of its owners or directors.
	CheckBusinessRegistry CheckType = "business_registry"
	// CheckDrivingLicense: the driving license is valid.
	CheckDrivingLicense CheckType = "driving_license"
	// CheckSIMSwap: the phone number has not been SIM-swapped recently.
	CheckSIMSwap CheckType = "sim_swap"
	// CheckMobileMoneyName: the mobile-money account behind the phone is
	// registered to the subject's name.
	CheckMobileMoneyName CheckType = "mobile_money_name"
)

// Outcome is a provider's answer to one check.
type Outcome string

const (
	// OutcomePass: the provider confirmed the claim.
	OutcomePass Outcome = "pass"
	// OutcomeFail: the provider gave an authoritative negative answer
	// (record not found, belongs to someone else, liveness failed).
	OutcomeFail Outcome = "fail"
	// OutcomeInconclusive: the provider answered but the answer neither
	// confirms nor refutes the claim (a near-miss name, a check that does
	// not apply to this kind of subject). A human should look.
	OutcomeInconclusive Outcome = "inconclusive"
	// OutcomeUnavailable: no provider could answer. Never read as a pass.
	OutcomeUnavailable Outcome = "unavailable"
)

// SubjectKind distinguishes a person from a business entity.
type SubjectKind string

const (
	// KindIndividual is a person, including a sole proprietor whose tax
	// PIN is their own.
	KindIndividual SubjectKind = "individual"
	// KindBusiness is a company or partnership with its own tax PIN.
	KindBusiness SubjectKind = "business"
)

// DocumentType names an image or file a provider may need.
type DocumentType string

const (
	DocumentIDFront        DocumentType = "id_front"
	DocumentIDBack         DocumentType = "id_back"
	DocumentSelfie         DocumentType = "selfie"
	DocumentDrivingLicense DocumentType = "driving_license"
)

// Document is a file the subject submitted, reachable at URL for a
// limited time (for example a presigned storage URL).
type Document struct {
	Type DocumentType
	URL  string
}

// Subject is who is being checked. Fields a check does not need may be
// empty; a provider that needs a missing field returns ErrInvalidSubject.
type Subject struct {
	// Reference is the caller's identifier for the subject, passed to
	// providers that accept one so their records can be correlated.
	Reference string
	Kind      SubjectKind
	// Country is the ISO 3166-1 alpha-2 code of the issuing country ("KE").
	Country string

	FullName    string
	NationalID  string
	DateOfBirth string // YYYY-MM-DD
	Phone       string // E.164

	TaxPIN               string
	BusinessName         string
	BusinessRegistration string
	LicenseNumber        string

	Documents []Document
}

// Result is the answer to one check.
type Result struct {
	Check    CheckType
	Provider string
	Outcome  Outcome
	// Reason explains the outcome for a human reviewer.
	Reason string
	// Details carries what the provider returned that a reviewer may need
	// (the registered name, the PIN on record). Keys are provider-defined.
	Details   map[string]string
	CheckedAt time.Time
}

// Verifier is one provider's implementation of one or more checks.
//
// Verify returns a Result for any authoritative answer, including a
// negative one (OutcomeFail). It returns an error wrapping ErrUnavailable
// when the provider could not be asked or its answer could not be
// understood, which is what makes the Engine try the next provider.
type Verifier interface {
	// Provider is a short stable name ("kra", "smileid") recorded on every
	// Result.
	Provider() string
	// Supports reports whether this provider can run the check.
	Supports(check CheckType) bool
	Verify(ctx context.Context, check CheckType, subject *Subject) (Result, error)
}
