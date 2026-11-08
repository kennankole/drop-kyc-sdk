package kra

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/kennankole/drop-kyc-sdk/verification"
)

// ProviderName is recorded on every Result this provider returns.
const ProviderName = "kra"

// Detail keys on this provider's Results.
const (
	DetailRegisteredPIN  = "registered_pin"
	DetailRegisteredName = "registered_name"
	DetailTaxpayerType   = "taxpayer_type"
	DetailPINStatus      = "pin_status"
	DetailNameMatches    = "name_matches"
)

// Lookup is the KRA calls the Verifier needs; *Client implements it.
type Lookup interface {
	LookupPINByNationalID(ctx context.Context, nationalID string) (Taxpayer, error)
	LookupPIN(ctx context.Context, pin string) (PINRecord, error)
}

// Verifier answers verification.CheckNationalID (PIN Checker By ID) and
// verification.CheckTaxPIN (PIN Checker by PIN).
type Verifier struct {
	lookup Lookup
}

var _ verification.Verifier = (*Verifier)(nil)

// NewVerifier returns a Verifier over lookup (normally a *Client).
func NewVerifier(lookup Lookup) *Verifier {
	return &Verifier{lookup: lookup}
}

// Provider implements verification.Verifier.
func (*Verifier) Provider() string { return ProviderName }

// Supports implements verification.Verifier.
func (*Verifier) Supports(check verification.CheckType) bool {
	return check == verification.CheckNationalID || check == verification.CheckTaxPIN
}

// Verify implements verification.Verifier.
func (v *Verifier) Verify(ctx context.Context, check verification.CheckType, s *verification.Subject) (verification.Result, error) {
	switch check {
	case verification.CheckNationalID:
		return v.verifyNationalID(ctx, s)
	case verification.CheckTaxPIN:
		return v.verifyTaxPIN(ctx, s)
	case verification.CheckSelfieMatch, verification.CheckBusinessRegistry, verification.CheckDrivingLicense,
		verification.CheckSIMSwap, verification.CheckMobileMoneyName:
	}

	return verification.Result{}, fmt.Errorf("%w: %s", verification.ErrUnsupported, check)
}

// verifyNationalID asks KRA which taxpayer holds the person's national ID.
// A match means the ID number is real (KRA registers PINs against the
// population register) and belongs to someone with this name. It does not
// prove the applicant is that person; only a photo match does. A person
// with no KRA PIN is not found, so not found is inconclusive, never fail.
func (v *Verifier) verifyNationalID(ctx context.Context, s *verification.Subject) (verification.Result, error) {
	nationalID := verification.NormalizeID(s.NationalID)
	if nationalID == "" || strings.TrimSpace(s.FullName) == "" {
		return verification.Result{}, fmt.Errorf("%w: the KRA ID check needs the national ID and full name", verification.ErrInvalidSubject)
	}

	taxpayer, err := v.lookup.LookupPINByNationalID(ctx, nationalID)
	if errors.Is(err, ErrTaxpayerNotFound) {
		return verification.Result{
			Outcome: verification.OutcomeInconclusive,
			Reason:  "KRA has no taxpayer for this national ID; people without a KRA PIN are not found, so this does not show the ID is fake",
		}, nil
	}

	if err != nil {
		return verification.Result{}, unavailable(err)
	}

	nameMatches := verification.NamesMatch(s.FullName, taxpayer.Name)
	r := verification.Result{
		Outcome: verification.OutcomePass,
		Reason:  "the ID number is registered at KRA to this name",
		Details: map[string]string{
			DetailRegisteredPIN:  verification.NormalizeID(taxpayer.PIN),
			DetailRegisteredName: strings.TrimSpace(taxpayer.Name),
			DetailNameMatches:    strconv.FormatBool(nameMatches),
		},
	}

	if !nameMatches {
		r.Outcome = verification.OutcomeInconclusive
		r.Reason = "the ID number is registered at KRA, but to a name that differs from the submitted one"
	}

	return r, nil
}

// verifyTaxPIN asks KRA who holds the PIN. The PIN must exist and be
// registered to the subject: the business name for a business, the
// person's name for an individual (including a sole proprietor).
func (v *Verifier) verifyTaxPIN(ctx context.Context, s *verification.Subject) (verification.Result, error) {
	pin := verification.NormalizeID(s.TaxPIN)

	expectedName, match := s.FullName, verification.NamesMatch
	if s.Kind == verification.KindBusiness {
		expectedName, match = s.BusinessName, verification.BusinessNamesMatch
	}

	if pin == "" || strings.TrimSpace(expectedName) == "" {
		return verification.Result{}, fmt.Errorf("%w: the KRA PIN check needs the PIN and the name it should be registered to", verification.ErrInvalidSubject)
	}

	record, err := v.lookup.LookupPIN(ctx, pin)
	if errors.Is(err, ErrTaxpayerNotFound) {
		return verification.Result{Outcome: verification.OutcomeFail, Reason: "KRA has no taxpayer with this PIN"}, nil
	}

	if err != nil {
		return verification.Result{}, unavailable(err)
	}

	nameMatches := match(expectedName, record.Name)
	r := verification.Result{
		Outcome: verification.OutcomePass,
		Reason:  "the PIN is registered at KRA to this name",
		Details: map[string]string{
			DetailRegisteredName: strings.TrimSpace(record.Name),
			DetailTaxpayerType:   strings.TrimSpace(record.TaxpayerType),
			DetailPINStatus:      strings.TrimSpace(record.Status),
			DetailNameMatches:    strconv.FormatBool(nameMatches),
		},
	}

	switch {
	case !isActive(record.Status):
		r.Outcome = verification.OutcomeInconclusive
		r.Reason = "the PIN exists but its KRA status is " + strings.TrimSpace(record.Status)
	case !nameMatches:
		r.Outcome = verification.OutcomeInconclusive
		r.Reason = "the PIN exists but is registered to a name that differs from the submitted one"
	}

	return r, nil
}

// isActive reads KRA's PIN status. An empty status is not held against the
// PIN: the field's vocabulary is undocumented.
func isActive(status string) bool {
	s := strings.ToLower(strings.TrimSpace(status))

	return s == "" || s == "active"
}

func unavailable(err error) error {
	if errors.Is(err, verification.ErrUnavailable) {
		return err
	}

	return fmt.Errorf("%w: %w", verification.ErrUnavailable, err)
}
