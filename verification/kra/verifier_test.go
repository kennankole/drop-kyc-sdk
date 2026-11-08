package kra_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kennankole/drop-kyc-sdk/verification"
	"github.com/kennankole/drop-kyc-sdk/verification/kra"
)

type stubLookup struct {
	taxpayer kra.Taxpayer
	record   kra.PINRecord
	err      error
	gotID    string
	gotPIN   string
}

func (s *stubLookup) LookupPINByNationalID(_ context.Context, id string) (kra.Taxpayer, error) {
	s.gotID = id

	return s.taxpayer, s.err
}

func (s *stubLookup) LookupPIN(_ context.Context, pin string) (kra.PINRecord, error) {
	s.gotPIN = pin

	return s.record, s.err
}

func rider() *verification.Subject {
	return &verification.Subject{Kind: verification.KindIndividual, FullName: "Njeri, Mary Wanjiru", NationalID: "1234 5678"}
}

func company() *verification.Subject {
	return &verification.Subject{
		Kind: verification.KindBusiness, FullName: "Mary Njeri", NationalID: "12345678",
		TaxPIN: "p051234567x", BusinessName: "Eden Bites Ltd",
	}
}

func TestVerifier_NationalID(t *testing.T) {
	cases := map[string]struct {
		taxpayer kra.Taxpayer
		err      error
		want     verification.Outcome
	}{
		"registered to this name":    {kra.Taxpayer{PIN: "a123456789z", Name: "MARY WANJIRU NJERI"}, nil, verification.OutcomePass},
		"registered to another name": {kra.Taxpayer{PIN: "A1", Name: "MARY NJERI"}, nil, verification.OutcomeInconclusive},
		"no KRA PIN is not fake":     {kra.Taxpayer{}, kra.ErrTaxpayerNotFound, verification.OutcomeInconclusive},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			lookup := &stubLookup{taxpayer: tc.taxpayer, err: tc.err}

			r, err := kra.NewVerifier(lookup).Verify(context.Background(), verification.CheckNationalID, rider())
			require.NoError(t, err)
			assert.Equal(t, tc.want, r.Outcome)
			assert.NotEmpty(t, r.Reason)
			assert.Equal(t, "12345678", lookup.gotID, "the ID is normalized before the lookup")
		})
	}

	r, err := kra.NewVerifier(&stubLookup{taxpayer: kra.Taxpayer{PIN: "a123456789z", Name: " MARY WANJIRU NJERI "}}).
		Verify(context.Background(), verification.CheckNationalID, rider())
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		kra.DetailRegisteredPIN: "A123456789Z", kra.DetailRegisteredName: "MARY WANJIRU NJERI", kra.DetailNameMatches: "true",
	}, r.Details, "the rider's KRA PIN comes back for the reviewer")
}

func TestVerifier_TaxPIN(t *testing.T) {
	active := kra.PINRecord{PIN: "P051234567X", Name: "EDEN BITES LIMITED", TaxpayerType: "Non-Individual", Status: "Active"}

	cases := map[string]struct {
		subject *verification.Subject
		record  kra.PINRecord
		err     error
		want    verification.Outcome
	}{
		"company, its own name":   {company(), active, nil, verification.OutcomePass},
		"company, another name":   {company(), kra.PINRecord{PIN: "P0", Name: "OTHER FOODS LTD", Status: "Active"}, nil, verification.OutcomeInconclusive},
		"company, PIN not active": {company(), kra.PINRecord{PIN: "P0", Name: active.Name, Status: "Dormant"}, nil, verification.OutcomeInconclusive},
		"no such PIN":             {company(), kra.PINRecord{}, kra.ErrTaxpayerNotFound, verification.OutcomeFail},
		"sole proprietor, own name": {
			&verification.Subject{Kind: verification.KindIndividual, FullName: "Mary Njeri", TaxPIN: "A1", BusinessName: "Eden Bites"},
			kra.PINRecord{PIN: "A1", Name: "MARY NJERI"}, nil, verification.OutcomePass,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			lookup := &stubLookup{record: tc.record, err: tc.err}

			r, err := kra.NewVerifier(lookup).Verify(context.Background(), verification.CheckTaxPIN, tc.subject)
			require.NoError(t, err)
			assert.Equal(t, tc.want, r.Outcome, r.Reason)
			assert.Equal(t, verification.NormalizeID(tc.subject.TaxPIN), lookup.gotPIN)
		})
	}

	r, err := kra.NewVerifier(&stubLookup{record: active}).Verify(context.Background(), verification.CheckTaxPIN, company())
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		kra.DetailRegisteredName: "EDEN BITES LIMITED", kra.DetailTaxpayerType: "Non-Individual",
		kra.DetailPINStatus: "Active", kra.DetailNameMatches: "true",
	}, r.Details)
}

func TestVerifier_Errors(t *testing.T) {
	v := kra.NewVerifier(&stubLookup{err: errors.New("dial tcp: refused")})

	for _, check := range []verification.CheckType{verification.CheckNationalID, verification.CheckTaxPIN} {
		_, err := v.Verify(context.Background(), check, company())
		require.ErrorIs(t, err, verification.ErrUnavailable, "any lookup failure is fallback-worthy: %s", check)
	}

	_, err := v.Verify(context.Background(), verification.CheckNationalID, &verification.Subject{NationalID: "1"})
	require.ErrorIs(t, err, verification.ErrInvalidSubject, "no name to compare")

	_, err = v.Verify(context.Background(), verification.CheckTaxPIN,
		&verification.Subject{Kind: verification.KindBusiness, TaxPIN: "P0", FullName: "Owner"})
	require.ErrorIs(t, err, verification.ErrInvalidSubject, "a business PIN is compared with the business name")

	_, err = v.Verify(context.Background(), verification.CheckSelfieMatch, company())
	require.ErrorIs(t, err, verification.ErrUnsupported)

	assert.Equal(t, "kra", v.Provider())
	assert.True(t, v.Supports(verification.CheckNationalID))
	assert.True(t, v.Supports(verification.CheckTaxPIN))
	assert.False(t, v.Supports(verification.CheckSelfieMatch))
}
