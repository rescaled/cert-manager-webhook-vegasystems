package vegasystems

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

const (
	DefaultBaseURL = "https://kunden.vegasystems.de/base/api"
	userAgent      = "cert-manager-webhook-vegasystems"
)

type Client struct {
	HTTP    *http.Client
	BaseURL string
	User    string
	Key     string
}

type Record struct {
	ID    json.Number `json:"id"`
	Name  string      `json:"name"`
	Type  string      `json:"type"`
	Value string      `json:"value"`
	TTL   json.Number `json:"ttl"`
}

type apiEnvelope struct {
	Success    *bool                      `json:"m_success"`
	ObjData    map[string]json.RawMessage `json:"m_obj_data"`
	Response   json.RawMessage            `json:"m_response"`
	Message    string                     `json:"m_message"`
	Errors     json.RawMessage            `json:"m_errors"`
	ErrorCodes json.RawMessage            `json:"m_error_codes"`
}

type domainEntry struct {
	Domdata struct {
		Name string `json:"name"`
	} `json:"domdata"`
}

func New(user, key string) *Client {
	return &Client{
		HTTP:    http.DefaultClient,
		BaseURL: DefaultBaseURL,
		User:    user,
		Key:     key,
	}
}

func (c *Client) baseURL() string {
	if c.BaseURL == "" {
		return DefaultBaseURL
	}
	return c.BaseURL
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP == nil {
		return http.DefaultClient
	}
	return c.HTTP
}

func (c *Client) do(ctx context.Context, path string, params url.Values) (*apiEnvelope, error) {
	params.Set("user", c.User)
	params.Set("key", c.Key)

	endpoint := strings.TrimRight(c.baseURL(), "/") + path
	full := endpoint + "?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, full, nil)
	if err != nil {
		return nil, fmt.Errorf("vegasystems: build request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("vegasystems: GET %s: %w", redact(full), sanitizeURLError(err))
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("vegasystems: read response: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("vegasystems: GET %s returned %s: %s", redact(full), resp.Status, scrubCreds(truncate(body, 256)))
	}

	var env apiEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("vegasystems: decode response from %s: %w (body=%s)", redact(full), err, scrubCreds(truncate(body, 256)))
	}
	if env.Success != nil && !*env.Success {
		msg := envelopeText(env.Errors)
		if msg == "" {
			msg = strings.TrimSpace(env.Message)
		}
		if msg == "" {
			msg = "(no message)"
		}
		if codes := envelopeText(env.ErrorCodes); codes != "" {
			msg = fmt.Sprintf("%s (codes=%s)", msg, codes)
		}
		return &env, fmt.Errorf("vegasystems: api reported failure: %s", msg)
	}
	return &env, nil
}

func (c *Client) FindDomainID(ctx context.Context, domain string, customerID int) (string, error) {
	env, err := c.do(ctx, "/domain/2003", url.Values{
		"id": []string{fmt.Sprintf("%d", customerID)},
	})
	if err != nil {
		return "", err
	}
	want := strings.ToLower(strings.TrimSuffix(domain, "."))
	for id, raw := range env.ObjData {
		var entry domainEntry
		if err := json.Unmarshal(raw, &entry); err != nil {
			continue
		}
		got := strings.ToLower(strings.TrimSuffix(entry.Domdata.Name, "."))
		if got == want {
			return id, nil
		}
	}
	return "", fmt.Errorf("vegasystems: domain %q not found for customer %d", domain, customerID)
}

func (c *Client) ListRecords(ctx context.Context, domainID string) ([]Record, error) {
	env, err := c.do(ctx, "/dns", url.Values{
		"mode":   []string{"read"},
		"domain": []string{domainID},
	})
	if err != nil {
		return nil, err
	}
	if len(env.Response) == 0 || string(env.Response) == "null" {
		return nil, nil
	}
	var records []Record
	if err := json.Unmarshal(env.Response, &records); err != nil {
		return nil, fmt.Errorf("vegasystems: decode records: %w", err)
	}
	return records, nil
}

func (c *Client) CreateRecord(ctx context.Context, domainID, name, recType string, ttl int, value string) error {
	_, err := c.do(ctx, "/dns", url.Values{
		"mode":   []string{"create"},
		"domain": []string{domainID},
		"name":   []string{name},
		"type":   []string{recType},
		"ttl":    []string{fmt.Sprintf("%d", ttl)},
		"value":  []string{value},
	})
	return err
}

func (c *Client) DeleteRecord(ctx context.Context, domainID, recordID string) error {
	_, err := c.do(ctx, "/dns", url.Values{
		"mode":   []string{"delete"},
		"domain": []string{domainID},
		"id":     []string{recordID},
	})
	return err
}

func (r Record) RecordID() string {
	return string(r.ID)
}

func redact(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "<vegasystems url>"
	}
	q := u.Query()
	if q.Has("user") {
		q.Set("user", "REDACTED")
	}
	if q.Has("key") {
		q.Set("key", "REDACTED")
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "...(truncated)"
}

var brTagRE = regexp.MustCompile(`(?i)<br\s*/?\s*>`)

// envelopeText flattens a json.RawMessage that may be a JSON string ("oops"),
// an array (["a","b"]), an object ({"1":"a","2":"b"}) — all observed shapes
// the live VegaSystems API uses for m_errors / m_error_codes — or empty/null
// into a single human-readable line. Returns "" for inputs that carry no
// information. HTML "<br />" markers (used by the upstream for web rendering)
// are collapsed to "; ".
func envelopeText(raw json.RawMessage) string {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" || s == `""` || s == "[]" || s == "{}" {
		return ""
	}
	if str, ok := decodeString(raw); ok {
		return normalizeText(str)
	}
	if arr, ok := decodeStringSlice(raw); ok {
		return normalizeText(strings.Join(arr, "; "))
	}
	if obj, ok := decodeStringMap(raw); ok {
		keys := make([]string, 0, len(obj))
		for k := range obj {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, obj[k])
		}
		return normalizeText(strings.Join(parts, "; "))
	}
	// Unknown shape — surface the raw JSON so operators see something.
	return normalizeText(s)
}

func decodeString(raw json.RawMessage) (string, bool) {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false
	}
	return s, true
}

func decodeStringSlice(raw json.RawMessage) ([]string, bool) {
	var arr []string
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil, false
	}
	return arr, true
}

func decodeStringMap(raw json.RawMessage) (map[string]string, bool) {
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, false
	}
	return m, true
}

func normalizeText(s string) string {
	s = brTagRE.ReplaceAllString(s, "; ")
	for strings.Contains(s, "; ; ") {
		s = strings.ReplaceAll(s, "; ; ", "; ")
	}
	return strings.TrimRight(strings.TrimSpace(s), ";: ")
}

// sanitizeURLError returns an error whose Error()/Unwrap() chain never
// contains the user/key query parameters. Go's *url.Error prints the full
// request URL via its Error() method, so wrapping it with %w would re-inject
// the cleartext credentials regardless of what is interpolated alongside.
func sanitizeURLError(err error) error {
	var ue *url.Error
	if !errors.As(err, &ue) {
		return err
	}
	clone := *ue
	clone.URL = redact(ue.URL)
	return &clone
}

var credParamRE = regexp.MustCompile(`(?i)\b(user|key)=[^&\s"]+`)

// scrubCreds replaces user=... and key=... query fragments in arbitrary text.
// Used to defang response bodies that echo the request URL back.
func scrubCreds(s string) string {
	return credParamRE.ReplaceAllString(s, "$1=REDACTED")
}
