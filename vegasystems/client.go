package vegasystems

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const DefaultBaseURL = "https://kunden.vegasystems.de/base/api"

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
	TTL   int         `json:"ttl"`
}

type apiEnvelope struct {
	Success  *bool                      `json:"m_success"`
	ObjData  map[string]json.RawMessage `json:"m_obj_data"`
	Response json.RawMessage            `json:"m_response"`
	Message  string                     `json:"m_message"`
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

func (c *Client) do(path string, params url.Values) (*apiEnvelope, error) {
	params.Set("user", c.User)
	params.Set("key", c.Key)

	endpoint := strings.TrimRight(c.baseURL(), "/") + path
	full := endpoint + "?" + params.Encode()

	resp, err := c.httpClient().Get(full)
	if err != nil {
		return nil, fmt.Errorf("vegasystems: GET %s: %w", redact(full), err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("vegasystems: read response: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("vegasystems: GET %s returned %s: %s", redact(full), resp.Status, truncate(body, 256))
	}

	var env apiEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("vegasystems: decode response from %s: %w (body=%s)", redact(full), err, truncate(body, 256))
	}
	if env.Success != nil && !*env.Success {
		return &env, fmt.Errorf("vegasystems: api reported failure: %s", strings.TrimSpace(env.Message))
	}
	return &env, nil
}

func (c *Client) FindDomainID(domain string, customerID int) (string, error) {
	env, err := c.do("/domain/2003", url.Values{
		"id": []string{fmt.Sprintf("%d", customerID)},
	})
	if err != nil {
		return "", err
	}
	for id, raw := range env.ObjData {
		var entry domainEntry
		if err := json.Unmarshal(raw, &entry); err != nil {
			continue
		}
		if entry.Domdata.Name == domain {
			return id, nil
		}
	}
	return "", fmt.Errorf("vegasystems: domain %q not found for customer %d", domain, customerID)
}

func (c *Client) ListRecords(domainID string) ([]Record, error) {
	env, err := c.do("/dns", url.Values{
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

func (c *Client) CreateRecord(domainID, name, recType string, ttl int, value string) error {
	_, err := c.do("/dns", url.Values{
		"mode":   []string{"create"},
		"domain": []string{domainID},
		"name":   []string{name},
		"type":   []string{recType},
		"ttl":    []string{fmt.Sprintf("%d", ttl)},
		"value":  []string{value},
	})
	return err
}

func (c *Client) DeleteRecord(domainID, recordID string) error {
	_, err := c.do("/dns", url.Values{
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
