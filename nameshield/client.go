// Package nameshield implements a cert-manager ACME DNS01 solver webhook for
// the Nameshield DNS API v4 (https://docs.nameshield.net/).
package nameshield

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultAPIURL is the production DNS API base URL.
	DefaultAPIURL = "https://api.nameshield.net/dns/v4"
	// OTEAPIURL is the test (OTE) environment base URL.
	OTEAPIURL = "https://ote-api.nameshield.net/dns/v4"

	defaultTimeout  = 30 * time.Second
	maxAttempts     = 5
	defaultRetryMin = 500 * time.Millisecond
)

// ErrNotFound is returned when the API answers HTTP 404, typically when the
// referenced zone does not exist.
var ErrNotFound = fmt.Errorf("nameshield: resource not found (HTTP 404)")

// Client is a minimal client for the Nameshield DNS API v4.
type Client struct {
	scheme     string
	host       string
	pathPrefix string
	token      string
	httpClient *http.Client
}

// NewClient builds a Nameshield API client. baseURL must not have a trailing
// slash, for example https://api.nameshield.net/dns/v4.
func NewClient(baseURL, token string) (*Client, error) {
	if baseURL == "" {
		baseURL = DefaultAPIURL
	}
	u, err := url.Parse(strings.TrimSuffix(baseURL, "/"))
	if err != nil {
		return nil, fmt.Errorf("nameshield: invalid API URL %q: %w", baseURL, err)
	}
	scheme := u.Scheme
	if scheme == "" {
		scheme = "https"
	}
	return &Client{
		scheme:     scheme,
		host:       u.Host,
		pathPrefix: strings.TrimSuffix(u.Path, "/"),
		token:      token,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
	}, nil
}

// Record is a DNS record as returned by the Nameshield API.
type Record struct {
	ID   flexibleString `json:"id"`
	Name string         `json:"name"`
	Type string         `json:"type"`
	Data string         `json:"data"`
	TTL  int            `json:"ttl"`
}

// flexibleString decodes a JSON value that may be either a string or a number.
// The Nameshield documentation shows record IDs quoted as strings while
// describing them as integers; both forms are accepted.
type flexibleString string

// UnmarshalJSON implements json.Unmarshaler.
func (f *flexibleString) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "null" || s == "" {
		*f = ""
		return nil
	}
	*f = flexibleString(s)
	return nil
}

// String returns the underlying string value.
func (f flexibleString) String() string { return string(f) }

// inputRecord is the payload expected by POST /zones/{zonename}/records.
type inputRecord struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Data    string `json:"data"`
	TTL     *int   `json:"ttl,omitempty"`
	Comment string `json:"comment,omitempty"`
}

// envelope is the standard Nameshield API response wrapper.
type envelope struct {
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// apiError decodes the standard Nameshield error payload.
type apiError struct {
	Errors []struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
}

// ListRecords returns the TXT records of a zone, optionally filtered by
// record name. showUpdates includes records created or updated but not yet
// validated.
func (c *Client) ListRecords(ctx context.Context, zone, name string, showUpdates bool) ([]Record, error) {
	q := url.Values{}
	q.Set("type", "TXT")
	q.Set("limit", "500")
	if name != "" {
		q.Set("name", name)
	}
	if showUpdates {
		q.Set("show_updates", "true")
	}

	var results struct {
		Results []Record `json:"results"`
	}
	if err := c.do(ctx, http.MethodGet, "/zones/"+zone+"/records", q, nil, &results); err != nil {
		return nil, err
	}
	return results.Results, nil
}

// CreateTxtRecord creates a TXT record in the given zone.
// data must include the surrounding double quotes required by the API for
// TXT records, for example "\"some-value\"".
func (c *Client) CreateTxtRecord(ctx context.Context, zone, name, data string, ttl int) error {
	in := inputRecord{
		Name:    name,
		Type:    "TXT",
		Data:    data,
		Comment: "cert-manager ACME dns-01 challenge",
	}
	if ttl > 0 {
		t := ttl
		in.TTL = &t
	}
	var rec Record
	return c.do(ctx, http.MethodPost, "/zones/"+zone+"/records", nil, in, &rec)
}

// DeleteRecord deletes a record by ID.
func (c *Client) DeleteRecord(ctx context.Context, zone, id string) error {
	return c.do(ctx, http.MethodDelete, "/zones/"+zone+"/records/"+id, nil, nil, nil)
}

// ValidateZone applies all pending changes of the current token on the zone.
// If includeOtherUsers is true, changes made by other users or tokens are
// validated too.
func (c *Client) ValidateZone(ctx context.Context, zone string, includeOtherUsers bool) error {
	q := url.Values{}
	q.Set("include_other_users_changes", strconv.FormatBool(includeOtherUsers))
	return c.do(ctx, http.MethodPost, "/zones/"+zone+"/validate", q, nil, nil)
}

// ZoneExists reports whether the zone is accessible with the current token.
// It is used to discover the zone hosting a record when no zone is known.
func (c *Client) ZoneExists(ctx context.Context, zone string) (bool, error) {
	err := c.do(ctx, http.MethodGet, "/zones/"+zone+"/records", url.Values{"limit": []string{"1"}}, nil, nil)
	if err == nil {
		return true, nil
	}
	if err == ErrNotFound {
		return false, nil
	}
	return false, err
}

// errRetryable signals that a request failed with a status that is worth
// retrying (429 or 5xx).
var errRetryable = fmt.Errorf("nameshield: retryable HTTP status")

// do performs an HTTP request against the API with Bearer authentication,
// retrying on 429 and 5xx responses. out is an optional pointer to a struct
// matching the "data" element of the response envelope.
func (c *Client) do(ctx context.Context, method, path string, query url.Values, body any, out any) error {
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("nameshield: failed to encode request body: %w", err)
		}
	}

	u := &url.URL{
		Scheme:   c.scheme,
		Host:     c.host,
		Path:     c.pathPrefix + "/" + strings.TrimPrefix(path, "/"),
		RawQuery: query.Encode(),
	}

	wait := defaultRetryMin
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		var bodyReader io.Reader
		if payload != nil {
			bodyReader = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, u.String(), bodyReader)
		if err != nil {
			return fmt.Errorf("nameshield: failed to build request: %w", err)
		}
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		lastErr = c.doOnce(req, out)
		if lastErr == nil {
			return nil
		}
		if !errors.Is(lastErr, errRetryable) {
			return lastErr
		}
		if attempt == maxAttempts {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
			wait *= 2
		}
	}
	return fmt.Errorf("nameshield: giving up after %d attempts: %w", maxAttempts, lastErr)
}

// doOnce executes a single request and decodes the response.
func (c *Client) doOnce(req *http.Request, out any) error {
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("nameshield: HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated:
		if out == nil {
			return nil
		}
		var env envelope
		if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
			return fmt.Errorf("nameshield: failed to decode response: %w", err)
		}
		if len(env.Data) == 0 || string(env.Data) == "null" {
			return nil
		}
		if err := json.Unmarshal(env.Data, out); err != nil {
			return fmt.Errorf("nameshield: failed to decode data element: %w", err)
		}
		return nil
	case resp.StatusCode == http.StatusNotFound:
		return ErrNotFound
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		// The API sets a Retry-After header (seconds) on 429 responses.
		if resp.StatusCode == http.StatusTooManyRequests {
			if ra := resp.Header.Get("Retry-After"); ra != "" {
				if secs, err := strconv.Atoi(strings.Split(ra, ".")[0]); err == nil && secs > 0 && secs < 60 {
					time.Sleep(time.Duration(secs) * time.Second)
				}
			}
		}
		return errRetryable
	default:
		var apiErr apiError
		if err := json.NewDecoder(resp.Body).Decode(&apiErr); err == nil && len(apiErr.Errors) > 0 {
			msgs := make([]string, 0, len(apiErr.Errors))
			for _, e := range apiErr.Errors {
				msgs = append(msgs, fmt.Sprintf("%s: %s", e.Code, e.Message))
			}
			return fmt.Errorf("nameshield: API error (HTTP %d): %s", resp.StatusCode, strings.Join(msgs, "; "))
		}
		return fmt.Errorf("nameshield: unexpected HTTP status %d", resp.StatusCode)
	}
}
