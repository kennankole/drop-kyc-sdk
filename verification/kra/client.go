// Package kra is a verification.Verifier backed by the Kenya Revenue
// Authority's GavaConnect API (developer.go.ke), which is free for approved
// developers. It uses two GavaConnect APIs:
//
//   - PIN Checker By ID (/checker/v1/pin): national ID in, the taxpayer's
//     PIN and name out. Answers verification.CheckNationalID for people
//     (riders, merchant owners).
//   - PIN Checker by PIN (/checker/v1/pinbypin): PIN in, the registered
//     taxpayer's name, type and PIN status out. Answers
//     verification.CheckTaxPIN, for individuals and for entities
//     (companies, partnerships), whose PIN is their own.
//
// The request and response shapes follow KRA's published pages and
// open-source integrations; they have not yet been exercised against KRA
// itself. Anything the client does not recognize is reported as
// verification.ErrUnavailable, never as a pass, with the raw body kept in
// the error. Confirm the shapes in the sandbox before using production
// credentials.
package kra

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/kennankole/drop-kyc-sdk/verification"
)

// SandboxBaseURL is KRA's GavaConnect sandbox. Production credentials come
// with a production host once KRA approves the app.
const SandboxBaseURL = "https://sbx.kra.go.ke"

const (
	tokenPath    = "/v1/token/generate?grant_type=client_credentials" //nolint:gosec // G101: an endpoint path, not a credential
	pinByIDPath  = "/checker/v1/pin"
	pinByPINPath = "/checker/v1/pinbypin"

	// taxpayerTypeKenyan is the TaxpayerType for a Kenyan resident looked up
	// by national ID.
	taxpayerTypeKenyan = "KE"

	// errCodeNotFound is the ErrorCode KRA returns when the ID has no
	// taxpayer record.
	errCodeNotFound = "30002"

	// defaultTokenTTL is used when the token response omits expires_in.
	defaultTokenTTL = time.Hour
	// tokenRefreshMargin renews a token before KRA would reject it.
	tokenRefreshMargin = time.Minute

	maxBodyBytes  = 64 << 10
	maxDetailBody = 300
)

// ErrTaxpayerNotFound is returned when KRA has no taxpayer for the national
// ID or PIN looked up.
var ErrTaxpayerNotFound = errors.New("kra: no taxpayer registered for this ID or PIN")

// Taxpayer is what KRA returns for a national ID.
type Taxpayer struct {
	PIN  string
	Name string
}

// PINRecord is what KRA returns for a PIN.
type PINRecord struct {
	PIN  string
	Name string
	// TaxpayerType is KRA's classification of the taxpayer, as returned.
	TaxpayerType string
	// Status is KRA's PIN status ("Active", ...), as returned.
	Status string
}

// Client is the GavaConnect PIN checker. Safe for concurrent use.
type Client struct {
	baseURL        string
	consumerKey    string
	consumerSecret string
	http           *http.Client
	now            func() time.Time

	mu          sync.Mutex
	token       string
	tokenExpiry time.Time
}

// NewClient builds a client. baseURL defaults to the sandbox; httpClient
// defaults to one with a 10 s timeout (the check runs off the request path).
func NewClient(baseURL, consumerKey, consumerSecret string, httpClient *http.Client) (*Client, error) {
	if consumerKey == "" || consumerSecret == "" {
		return nil, errors.New("kra: consumer key and secret are required")
	}

	if baseURL == "" {
		baseURL = SandboxBaseURL
	}

	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}

	return &Client{
		baseURL:        strings.TrimRight(baseURL, "/"),
		consumerKey:    consumerKey,
		consumerSecret: consumerSecret,
		http:           httpClient,
		now:            time.Now,
	}, nil
}

type tokenResponse struct {
	AccessToken string          `json:"access_token"`
	ExpiresIn   json.RawMessage `json:"expires_in"`
}

type pinByIDRequest struct {
	TaxpayerType string `json:"TaxpayerType"`
	TaxpayerID   string `json:"TaxpayerID"`
}

type pinByIDResponse struct {
	TaxpayerPIN  string `json:"TaxpayerPIN"`
	TaxpayerName string `json:"TaxpayerName"`
	ErrorCode    string `json:"ErrorCode"`
	ErrorMessage string `json:"ErrorMessage"`
}

type pinByPINRequest struct {
	KRAPIN string `json:"KRAPIN"`
}

type pinByPINResponse struct {
	Message string `json:"Message"`
	PINData *struct {
		KRAPIN         string `json:"KRAPIN"`
		TypeOfTaxpayer string `json:"TypeOfTaxpayer"`
		Name           string `json:"Name"`
		StatusOfPIN    string `json:"StatusOfPIN"`
	} `json:"PINDATA"`
	ErrorCode    string `json:"ErrorCode"`
	ErrorMessage string `json:"ErrorMessage"`
}

// LookupPINByNationalID returns the taxpayer KRA holds for a Kenyan national
// ID, ErrTaxpayerNotFound, or an error wrapping verification.ErrUnavailable.
func (c *Client) LookupPINByNationalID(ctx context.Context, nationalID string) (Taxpayer, error) {
	body, status, err := c.post(ctx, pinByIDPath,
		pinByIDRequest{TaxpayerType: taxpayerTypeKenyan, TaxpayerID: nationalID})
	if err != nil {
		return Taxpayer{}, err
	}

	var resp pinByIDResponse
	if jsonErr := json.Unmarshal(body, &resp); jsonErr != nil {
		return Taxpayer{}, fmt.Errorf("%w: HTTP %d, body %q", verification.ErrUnavailable, status, clip(body))
	}

	if resp.ErrorCode == errCodeNotFound {
		return Taxpayer{}, ErrTaxpayerNotFound
	}

	if status != http.StatusOK || resp.ErrorCode != "" || resp.ErrorMessage != "" || resp.TaxpayerPIN == "" {
		return Taxpayer{}, fmt.Errorf("%w: HTTP %d, body %q", verification.ErrUnavailable, status, clip(body))
	}

	return Taxpayer{PIN: resp.TaxpayerPIN, Name: resp.TaxpayerName}, nil
}

// LookupPIN returns what KRA holds for a PIN, ErrTaxpayerNotFound, or an
// error wrapping verification.ErrUnavailable.
//
// This API's not-found answer is not documented. An ErrorCode of 30002 (the
// By-ID API's not-found code) or an ErrorMessage saying the PIN is invalid
// or unknown is read as not found; any other error is unavailable, so a KRA
// fault is never mistaken for a fake PIN.
func (c *Client) LookupPIN(ctx context.Context, pin string) (PINRecord, error) {
	body, status, err := c.post(ctx, pinByPINPath, pinByPINRequest{KRAPIN: pin})
	if err != nil {
		return PINRecord{}, err
	}

	var resp pinByPINResponse
	if jsonErr := json.Unmarshal(body, &resp); jsonErr != nil {
		return PINRecord{}, fmt.Errorf("%w: HTTP %d, body %q", verification.ErrUnavailable, status, clip(body))
	}

	if resp.ErrorCode == errCodeNotFound || saysNotFound(resp.ErrorMessage) {
		return PINRecord{}, ErrTaxpayerNotFound
	}

	if status != http.StatusOK || resp.ErrorCode != "" || resp.ErrorMessage != "" ||
		resp.PINData == nil || resp.PINData.KRAPIN == "" {
		return PINRecord{}, fmt.Errorf("%w: HTTP %d, body %q", verification.ErrUnavailable, status, clip(body))
	}

	d := resp.PINData

	return PINRecord{PIN: d.KRAPIN, Name: d.Name, TaxpayerType: d.TypeOfTaxpayer, Status: d.StatusOfPIN}, nil
}

// notFoundPhrases is KRA's likely wording for an unknown PIN.
var notFoundPhrases = []string{"invalid pin", "not found", "does not exist", "no record", "not registered"}

func saysNotFound(msg string) bool {
	m := strings.ToLower(msg)

	for _, phrase := range notFoundPhrases {
		if strings.Contains(m, phrase) {
			return true
		}
	}

	return false
}

// post sends an authenticated JSON request. A 401 means the token was
// revoked or expired early, so it is retried once with a fresh token.
func (c *Client) post(ctx context.Context, path string, payload any) (body []byte, status int, err error) {
	body, status, err = c.postOnce(ctx, path, payload)
	if err != nil || status != http.StatusUnauthorized {
		return body, status, err
	}

	c.clearToken()

	return c.postOnce(ctx, path, payload)
}

func (c *Client) postOnce(ctx context.Context, path string, payload any) (body []byte, status int, err error) {
	token, err := c.accessToken(ctx)
	if err != nil {
		return nil, 0, err
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, fmt.Errorf("kra: encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(encoded))
	if err != nil {
		return nil, 0, fmt.Errorf("kra: build request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	return c.do(req)
}

// accessToken returns a cached token, fetching one with the consumer key and
// secret as HTTP Basic credentials when none is valid.
func (c *Client) accessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.token != "" && c.now().Before(c.tokenExpiry) {
		return c.token, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.baseURL+tokenPath, http.NoBody)
	if err != nil {
		return "", fmt.Errorf("kra: build token request: %w", err)
	}

	req.SetBasicAuth(c.consumerKey, c.consumerSecret)

	body, status, err := c.do(req)
	if err != nil {
		return "", err
	}

	var tok tokenResponse
	if status != http.StatusOK || json.Unmarshal(body, &tok) != nil || tok.AccessToken == "" {
		return "", fmt.Errorf("%w: kra token request failed: HTTP %d, body %q", verification.ErrUnavailable, status, clip(body))
	}

	ttl := parseExpiresIn(tok.ExpiresIn)
	c.token = tok.AccessToken
	c.tokenExpiry = c.now().Add(max(ttl-tokenRefreshMargin, tokenRefreshMargin))

	return c.token, nil
}

func (c *Client) clearToken() {
	c.mu.Lock()
	c.token = ""
	c.mu.Unlock()
}

func (c *Client) do(req *http.Request) (body []byte, status int, err error) {
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: kra %s %s: %w", verification.ErrUnavailable, req.Method, req.URL.Path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err = io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("%w: kra read response: %w", verification.ErrUnavailable, err)
	}

	return body, resp.StatusCode, nil
}

// parseExpiresIn accepts expires_in as a number or a numeric string, which
// OAuth servers send interchangeably.
func parseExpiresIn(raw json.RawMessage) time.Duration {
	var seconds json.Number
	if json.Unmarshal(raw, &seconds) != nil {
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return defaultTokenTTL
		}

		seconds = json.Number(s)
	}

	n, err := seconds.Int64()
	if err != nil || n <= 0 {
		return defaultTokenTTL
	}

	return time.Duration(n) * time.Second
}

func clip(body []byte) string {
	s := strings.TrimSpace(string(body))
	if len(s) > maxDetailBody {
		return s[:maxDetailBody] + "…"
	}

	return s
}
