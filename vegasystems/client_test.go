package vegasystems

import (
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
	id, err := c.FindDomainID("example.com", 42)
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
	if _, err := c.FindDomainID("example.com", 1); err == nil {
		t.Fatal("expected error for missing domain")
	}
}

func TestApiFailure(t *testing.T) {
	_, c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"m_success":false,"m_message":"bad creds"}`)
	})
	_, err := c.FindDomainID("example.com", 1)
	if err == nil || !strings.Contains(err.Error(), "bad creds") {
		t.Fatalf("expected api failure error, got %v", err)
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
	recs, err := c.ListRecords("100")
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

func TestListRecordsEmpty(t *testing.T) {
	_, c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"m_success":true,"m_response":null}`)
	})
	recs, err := c.ListRecords("100")
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
	if err := c.CreateRecord("100", "_acme-challenge", "TXT", 120, "secret-key"); err != nil {
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
	if err := c.DeleteRecord("100", "7"); err != nil {
		t.Fatalf("DeleteRecord: %v", err)
	}
}

func TestNon2xx(t *testing.T) {
	_, c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	_, err := c.FindDomainID("example.com", 1)
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
