package kra_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kennankole/drop-kyc-sdk/verification"
	"github.com/kennankole/drop-kyc-sdk/verification/kra"
)

type fakeKRA struct {
	tokens    atomic.Int32
	expiresIn any
	pinStatus int
	pinBody   string
	gotReq    map[string]string
	gotAuth   string
	gotPath   string
}

func (f *fakeKRA) handler(t *testing.T) http.Handler {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/token/generate", func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		assert.True(t, ok && user == "key" && pass == "secret", "consumer key and secret as Basic auth")
		assert.Equal(t, "client_credentials", r.URL.Query().Get("grant_type"))

		f.tokens.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "tok", "expires_in": f.expiresIn})
	})
	answer := func(w http.ResponseWriter, r *http.Request) {
		f.gotAuth = r.Header.Get("Authorization")
		f.gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&f.gotReq)

		w.WriteHeader(f.pinStatus)
		_, _ = w.Write([]byte(f.pinBody))
	}
	mux.HandleFunc("POST /checker/v1/pin", answer)
	mux.HandleFunc("POST /checker/v1/pinbypin", answer)

	return mux
}

func newClient(t *testing.T, f *fakeKRA) *kra.Client {
	t.Helper()

	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)

	c, err := kra.NewClient(srv.URL+"/", "key", "secret", srv.Client())
	require.NoError(t, err)

	return c
}

func TestLookupPINByNationalID_Found(t *testing.T) {
	f := &fakeKRA{expiresIn: "3599", pinStatus: http.StatusOK,
		pinBody: `{"TaxpayerPIN":"A123456789Z","TaxpayerName":"MARY NJERI"}`}
	c := newClient(t, f)

	got, err := c.LookupPINByNationalID(context.Background(), "41789723")
	require.NoError(t, err)
	assert.Equal(t, kra.Taxpayer{PIN: "A123456789Z", Name: "MARY NJERI"}, got)
	assert.Equal(t, map[string]string{"TaxpayerType": "KE", "TaxpayerID": "41789723"}, f.gotReq)
	assert.Equal(t, "Bearer tok", f.gotAuth)

	_, err = c.LookupPINByNationalID(context.Background(), "41789723")
	require.NoError(t, err)
	assert.Equal(t, int32(1), f.tokens.Load(), "the token is cached")
}

func TestLookupPINByNationalID_NotFound(t *testing.T) {
	f := &fakeKRA{expiresIn: 3600, pinStatus: http.StatusBadRequest,
		pinBody: `{"ErrorCode":"30002","ErrorMessage":"No PIN found"}`}

	_, err := newClient(t, f).LookupPINByNationalID(context.Background(), "1")
	require.ErrorIs(t, err, kra.ErrTaxpayerNotFound)
}

func TestLookupPINByNationalID_UnknownShapeIsNotAPass(t *testing.T) {
	cases := map[string]struct {
		status int
		body   string
	}{
		"html error page": {http.StatusBadGateway, "<html>gateway</html>"},
		"other error":     {http.StatusOK, `{"ErrorCode":"500","ErrorMessage":"system error"}`},
		"empty PIN":       {http.StatusOK, `{"TaxpayerName":"X"}`},
		"different shape": {http.StatusOK, `{"PINDATA":{"KRAPIN":"A1"}}`},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := &fakeKRA{pinStatus: tc.status, pinBody: tc.body}

			_, err := newClient(t, f).LookupPINByNationalID(context.Background(), "1")
			require.ErrorIs(t, err, verification.ErrUnavailable)
			assert.NotErrorIs(t, err, kra.ErrTaxpayerNotFound)
		})
	}
}

func TestLookupPINByNationalID_RefreshesTokenOn401(t *testing.T) {
	f := &fakeKRA{pinStatus: http.StatusUnauthorized, pinBody: `{}`}

	_, err := newClient(t, f).LookupPINByNationalID(context.Background(), "1")
	require.Error(t, err)
	assert.Equal(t, int32(2), f.tokens.Load(), "one retry with a fresh token")
}

func TestNewClient_RequiresCredentials(t *testing.T) {
	_, err := kra.NewClient("", "", "secret", nil)
	require.Error(t, err)

	c, err := kra.NewClient("", "k", "s", nil)
	require.NoError(t, err)
	assert.NotNil(t, c)
}

func TestLookupPIN_Found(t *testing.T) {
	f := &fakeKRA{expiresIn: 3600, pinStatus: http.StatusOK, pinBody: `{"Message":"Valid PIN","PINDATA":{
		"KRAPIN":"P051234567X","TypeOfTaxpayer":"Non-Individual","Name":"EDEN BITES LIMITED","StatusOfPIN":"Active"}}`}

	got, err := newClient(t, f).LookupPIN(context.Background(), "P051234567X")
	require.NoError(t, err)
	assert.Equal(t, kra.PINRecord{PIN: "P051234567X", Name: "EDEN BITES LIMITED", TaxpayerType: "Non-Individual", Status: "Active"}, got)
	assert.Equal(t, "/checker/v1/pinbypin", f.gotPath)
	assert.Equal(t, map[string]string{"KRAPIN": "P051234567X"}, f.gotReq)
	assert.Equal(t, "Bearer tok", f.gotAuth)
}

func TestLookupPIN_NotFound(t *testing.T) {
	for name, body := range map[string]string{
		"error code":    `{"ErrorCode":"30002","ErrorMessage":"x"}`,
		"error message": `{"ErrorMessage":"Invalid PIN"}`,
	} {
		t.Run(name, func(t *testing.T) {
			f := &fakeKRA{pinStatus: http.StatusOK, pinBody: body}

			_, err := newClient(t, f).LookupPIN(context.Background(), "A1")
			require.ErrorIs(t, err, kra.ErrTaxpayerNotFound)
		})
	}
}

func TestLookupPIN_UnknownShapeIsUnavailable(t *testing.T) {
	for name, tc := range map[string]struct {
		status int
		body   string
	}{
		"system error":  {http.StatusOK, `{"ErrorMessage":"System error, try later"}`},
		"no PINDATA":    {http.StatusOK, `{"Message":"ok"}`},
		"by-ID shape":   {http.StatusOK, `{"TaxpayerPIN":"A1"}`},
		"gateway error": {http.StatusBadGateway, "<html>bad gateway</html>"},
	} {
		t.Run(name, func(t *testing.T) {
			f := &fakeKRA{pinStatus: tc.status, pinBody: tc.body}

			_, err := newClient(t, f).LookupPIN(context.Background(), "A1")
			require.ErrorIs(t, err, verification.ErrUnavailable)
			assert.NotErrorIs(t, err, kra.ErrTaxpayerNotFound)
		})
	}
}
