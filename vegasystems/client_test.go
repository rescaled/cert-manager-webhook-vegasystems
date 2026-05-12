package vegasystems

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *Client) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c := New("u", "k")
	c.BaseURL = srv.URL
	return srv, c
}

func TestFindDomainID(t *testing.T) {
	_, c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/domain/2003" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("user") != "u" || q.Get("key") != "k" {
			t.Errorf("missing creds: %v", q)
		}
		if q.Get("id") != "42" {
			t.Errorf("unexpected customer id %q", q.Get("id"))
		}
		fmt.Fprint(w, `{"m_success":true,"m_obj_data":{"100":{"domdata":{"name":"example.com"}},"101":{"domdata":{"name":"other.com"}}}}`)
	})
	id, err := c.FindDomainID(context.Background(), "example.com", 42)
	if err != nil {
		t.Fatalf("FindDomainID: %v", err)
	}
	if id != "100" {
		t.Errorf("got %q want 100", id)
	}
}

func TestFindDomainIDNotFound(t *testing.T) {
	_, c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"m_success":true,"m_obj_data":{"100":{"domdata":{"name":"other.com"}}}}`)
	})
	if _, err := c.FindDomainID(context.Background(), "example.com", 1); err == nil {
		t.Fatal("expected error for missing domain")
	}
}

func TestApiFailure(t *testing.T) {
	_, c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"m_success":false,"m_message":"bad creds"}`)
	})
	_, err := c.FindDomainID(context.Background(), "example.com", 1)
	if err == nil || !strings.Contains(err.Error(), "bad creds") {
		t.Fatalf("expected api failure error, got %v", err)
	}
}

func TestApiFailureMErrorsString(t *testing.T) {
	_, c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"m_success":false,"m_errors":"bad credentials","m_error_codes":[]}`)
	})
	_, err := c.FindDomainID(context.Background(), "example.com", 1)
	if err == nil || !strings.Contains(err.Error(), "bad credentials") {
		t.Fatalf("expected m_errors string in error, got %v", err)
	}
}

func TestApiFailureMErrorsArray(t *testing.T) {
	_, c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"m_success":false,"m_errors":["bad credentials","domain locked"]}`)
	})
	_, err := c.FindDomainID(context.Background(), "example.com", 1)
	if err == nil {
		t.Fatal("expected api failure error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "bad credentials") || !strings.Contains(msg, "domain locked") {
		t.Errorf("expected both array entries in error, got %q", msg)
	}
}

func TestApiFailurePrefersMErrors(t *testing.T) {
	_, c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"m_success":false,"m_errors":"specific","m_message":"generic"}`)
	})
	_, err := c.FindDomainID(context.Background(), "example.com", 1)
	if err == nil {
		t.Fatal("expected api failure error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "specific") {
		t.Errorf("expected m_errors to win, got %q", msg)
	}
	if strings.Contains(msg, "generic") {
		t.Errorf("did not expect m_message when m_errors is present, got %q", msg)
	}
}

func TestApiFailureIncludesCodes(t *testing.T) {
	_, c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"m_success":false,"m_errors":"oops","m_error_codes":["E_NO_AUTH"]}`)
	})
	_, err := c.FindDomainID(context.Background(), "example.com", 1)
	if err == nil || !strings.Contains(err.Error(), "E_NO_AUTH") {
		t.Fatalf("expected error codes in message, got %v", err)
	}
}

func TestApiFailureMErrorsObject(t *testing.T) {
	// The live API can return m_errors as an object keyed by numeric strings,
	// each value being an error message (e.g. for field-level validation
	// errors). Embedded "<br />" HTML should be normalized away.
	_, c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"m_success":false,"m_errors":{"1":"first problem<br />","2":"second problem"}}`)
	})
	_, err := c.FindDomainID(context.Background(), "example.com", 1)
	if err == nil {
		t.Fatal("expected api failure error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "first problem") || !strings.Contains(msg, "second problem") {
		t.Errorf("expected both object values in error, got %q", msg)
	}
	if strings.Contains(msg, "<br") {
		t.Errorf("expected <br /> to be stripped from error, got %q", msg)
	}
}

func TestCreateRecordRealWorldShape(t *testing.T) {
	// Mirrors the *shape* of a real /dns?mode=create success response:
	// m_response carries a status string (not the new ID), m_id and
	// m_obj_data are null. CreateRecord must report success.
	_, c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"m_success":true,"m_id":null,"m_response":"example success message","m_errors":[],"m_error_codes":[],"m_obj_data":null}`)
	})
	if err := c.CreateRecord(context.Background(), "99999", "_acme-challenge.www", "TXT", 300, "example-key"); err != nil {
		t.Fatalf("CreateRecord: %v", err)
	}
}

func TestDeleteRecordRealWorldShape(t *testing.T) {
	// Mirrors the *shape* of a real /dns?mode=delete success response.
	_, c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"m_success":true,"m_id":null,"m_response":"example success message","m_errors":[],"m_error_codes":[],"m_obj_data":null}`)
	})
	if err := c.DeleteRecord(context.Background(), "99999", "1001"); err != nil {
		t.Fatalf("DeleteRecord: %v", err)
	}
}

func TestApiFailureEmpty(t *testing.T) {
	_, c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"m_success":false}`)
	})
	_, err := c.FindDomainID(context.Background(), "example.com", 1)
	if err == nil || !strings.Contains(err.Error(), "(no message)") {
		t.Fatalf("expected '(no message)' fallback, got %v", err)
	}
}

func TestListRecords(t *testing.T) {
	_, c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("mode") != "read" || q.Get("domain") != "100" {
			t.Errorf("bad query: %v", q)
		}
		fmt.Fprint(w, `{"m_success":true,"m_response":[
			{"id":7,"name":"_acme-challenge","type":"TXT","value":"abc","ttl":120},
			{"id":"8","name":"_acme-challenge","type":"TXT","value":"def","ttl":120}
		]}`)
	})
	recs, err := c.ListRecords(context.Background(), "100")
	if err != nil {
		t.Fatalf("ListRecords: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("got %d records, want 2", len(recs))
	}
	if recs[0].RecordID() != "7" || recs[1].RecordID() != "8" {
		t.Errorf("unexpected ids: %q, %q", recs[0].RecordID(), recs[1].RecordID())
	}
	if recs[0].Value != "abc" {
		t.Errorf("value mismatch: %q", recs[0].Value)
	}
}

func TestListRecordsRealWorldShape(t *testing.T) {
	// Mirrors the *shape* of a real /dns?mode=read response from the live
	// VegaSystems API: id and ttl arrive as JSON strings (not numbers), each
	// record carries an extra "domain" field, m_obj_data is null. All values
	// below are synthetic — IDs from a fake range, IP from RFC 5737 TEST-NET-1.
	body := `{
		"m_success": true,
		"m_response": [
			{"id":"1001","name":"","value":"192.0.2.1","ttl":"600","type":"A","domain":"99999"},
			{"id":"1002","name":"www","value":"192.0.2.1","ttl":"600","type":"A","domain":"99999"}
		],
		"m_obj_data": null,
		"m_errors": [],
		"m_error_codes": []
	}`
	_, c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, body)
	})
	recs, err := c.ListRecords(context.Background(), "99999")
	if err != nil {
		t.Fatalf("ListRecords: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("got %d records, want 2", len(recs))
	}
	if recs[0].RecordID() != "1001" || recs[1].RecordID() != "1002" {
		t.Errorf("unexpected ids: %q, %q", recs[0].RecordID(), recs[1].RecordID())
	}
	if recs[1].Name != "www" || recs[1].Type != "A" {
		t.Errorf("unexpected record contents: %+v", recs[1])
	}
}

func TestApiFailureRealWorldShape(t *testing.T) {
	// Mirrors the *shape* of a real error response: m_errors as a plain string,
	// m_error_codes as the empty array, all other fields null. Synthetic
	// message text — the real API may return any localized string.
	_, c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"m_success":false,"m_id":null,"m_response":null,"m_errors":"example api error","m_error_codes":[],"m_obj_data":null,"m_console_out":null,"m_data":[]}`)
	})
	_, err := c.FindDomainID(context.Background(), "example.com", 1)
	if err == nil {
		t.Fatal("expected api failure error, got nil")
	}
	if !strings.Contains(err.Error(), "example api error") {
		t.Errorf("expected m_errors text in message, got %q", err)
	}
	// codes==[] must NOT produce a "(codes=...)" suffix.
	if strings.Contains(err.Error(), "codes=") {
		t.Errorf("did not expect codes suffix for empty m_error_codes, got %q", err)
	}
}

func TestListRecordsEmpty(t *testing.T) {
	_, c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"m_success":true,"m_response":null}`)
	})
	recs, err := c.ListRecords(context.Background(), "100")
	if err != nil {
		t.Fatalf("ListRecords: %v", err)
	}
	if len(recs) != 0 {
		t.Errorf("expected zero records, got %d", len(recs))
	}
}

func TestCreateRecord(t *testing.T) {
	_, c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		want := map[string]string{
			"mode":   "create",
			"domain": "100",
			"name":   "_acme-challenge",
			"type":   "TXT",
			"ttl":    "120",
			"value":  "secret-key",
		}
		for k, v := range want {
			if q.Get(k) != v {
				t.Errorf("query %q=%q, want %q", k, q.Get(k), v)
			}
		}
		fmt.Fprint(w, `{"m_success":true}`)
	})
	if err := c.CreateRecord(context.Background(), "100", "_acme-challenge", "TXT", 120, "secret-key"); err != nil {
		t.Fatalf("CreateRecord: %v", err)
	}
}

func TestDeleteRecord(t *testing.T) {
	_, c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("mode") != "delete" || q.Get("domain") != "100" || q.Get("id") != "7" {
			t.Errorf("bad query: %v", q)
		}
		fmt.Fprint(w, `{"m_success":true}`)
	})
	if err := c.DeleteRecord(context.Background(), "100", "7"); err != nil {
		t.Fatalf("DeleteRecord: %v", err)
	}
}

func TestNon2xx(t *testing.T) {
	_, c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	_, err := c.FindDomainID(context.Background(), "example.com", 1)
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("expected 500 error, got %v", err)
	}
}

func TestRedactCredentials(t *testing.T) {
	got := redact("https://api.example/path?user=secret&key=topsecret&domain=100")
	if strings.Contains(got, "secret") || strings.Contains(got, "topsecret") {
		t.Errorf("redact leaked: %s", got)
	}
	if !strings.Contains(got, "REDACTED") {
		t.Errorf("redact missing replacement: %s", got)
	}
}

func TestTransportErrorRedaction(t *testing.T) {
	// Point at a port that nothing's listening on. Transport will fail, and
	// the resulting *url.Error normally embeds the full URL (including
	// user=/key= query params) in its Error() string.
	c := New("super-secret-user", "super-secret-key")
	c.BaseURL = "http://127.0.0.1:1"
	_, err := c.FindDomainID(context.Background(), "example.com", 1)
	if err == nil {
		t.Fatal("expected transport error, got nil")
	}
	msg := err.Error()
	if strings.Contains(msg, "super-secret-user") || strings.Contains(msg, "super-secret-key") {
		t.Errorf("credentials leaked in error: %s", msg)
	}
}

func TestNon2xxBodyScrubbing(t *testing.T) {
	_, c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		// Echo a body containing the credential query fragment, as a poorly
		// written upstream might do in a 500 error page.
		http.Error(w, "upstream: bad request to /dns?user=super-secret-user&key=super-secret-key&mode=read", http.StatusInternalServerError)
	})
	_, err := c.FindDomainID(context.Background(), "example.com", 1)
	if err == nil {
		t.Fatal("expected 5xx error, got nil")
	}
	msg := err.Error()
	if strings.Contains(msg, "super-secret-user") || strings.Contains(msg, "super-secret-key") {
		t.Errorf("credentials leaked in 5xx body: %s", msg)
	}
}

func TestDecodeFailureBodyScrubbing(t *testing.T) {
	_, c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		// 200 OK but garbage body that mentions creds.
		fmt.Fprint(w, `<html>upstream is sad about user=super-secret-user&key=super-secret-key</html>`)
	})
	_, err := c.FindDomainID(context.Background(), "example.com", 1)
	if err == nil {
		t.Fatal("expected decode error, got nil")
	}
	msg := err.Error()
	if strings.Contains(msg, "super-secret-user") || strings.Contains(msg, "super-secret-key") {
		t.Errorf("credentials leaked in decode-failure body: %s", msg)
	}
}

func TestFindDomainIDCaseAndTrailingDot(t *testing.T) {
	cases := []struct {
		name    string
		query   string
		apiName string
	}{
		{"trailing-dot-on-api", "example.com", "Example.Com."},
		{"trailing-dot-on-query", "Example.com.", "example.com"},
		{"all-uppercase-on-api", "example.com", "EXAMPLE.COM"},
		{"identical-lowercase", "example.com", "example.com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintf(w, `{"m_success":true,"m_obj_data":{"100":{"domdata":{"name":%q}}}}`, tc.apiName)
			})
			id, err := c.FindDomainID(context.Background(), tc.query, 1)
			if err != nil {
				t.Fatalf("FindDomainID: %v", err)
			}
			if id != "100" {
				t.Errorf("got %q want 100", id)
			}
		})
	}
}

func TestUserAgentHeader(t *testing.T) {
	var gotUA string
	_, c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		fmt.Fprint(w, `{"m_success":true,"m_obj_data":{}}`)
	})
	if _, err := c.FindDomainID(context.Background(), "x", 1); err != nil {
		// FindDomainID will return "not found"; we only care about the UA header
		_ = err
	}
	if !strings.HasPrefix(gotUA, "cert-manager-webhook-vegasystems") {
		t.Errorf("User-Agent = %q, want prefix %q", gotUA, "cert-manager-webhook-vegasystems")
	}
}

func TestContextCancellation(t *testing.T) {
	_, c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"m_success":true,"m_obj_data":{}}`)
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.FindDomainID(ctx, "x", 1)
	if err == nil {
		t.Fatal("expected error on cancelled context, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled in chain", err)
	}
}
