package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	extapi "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/cert-manager/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"
)

func TestCleanUpAggregatesDeleteErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case r.URL.Path == "/domain/2003":
			fmt.Fprint(w, `{"m_success":true,"m_obj_data":{"D":{"domdata":{"name":"example.com"}}}}`)
		case r.URL.Path == "/dns" && q.Get("mode") == "read":
			fmt.Fprint(w, `{"m_success":true,"m_response":[
				{"id":7,"name":"_acme-challenge","type":"TXT","value":"the-key","ttl":120},
				{"id":8,"name":"_acme-challenge","type":"TXT","value":"the-key","ttl":120},
				{"id":9,"name":"_acme-challenge","type":"TXT","value":"the-key","ttl":120}
			]}`)
		case r.URL.Path == "/dns" && q.Get("mode") == "delete":
			id := q.Get("id")
			if id == "7" || id == "9" {
				http.Error(w, `{"m_success":false,"m_message":"upstream boom"}`, http.StatusInternalServerError)
				return
			}
			fmt.Fprint(w, `{"m_success":true}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "creds", Namespace: "default"},
		Data:       map[string][]byte{"u": []byte("user-val"), "k": []byte("key-val")},
	}
	s := &solver{
		kube: fake.NewSimpleClientset(secret),
		http: http.DefaultClient,
	}

	cfg := fmt.Sprintf(`{
		"customerId": 42,
		"baseURL": %q,
		"apiUserSecretRef": {"name":"creds","key":"u"},
		"apiKeySecretRef":  {"name":"creds","key":"k"}
	}`, srv.URL)

	ch := &v1alpha1.ChallengeRequest{
		Config:            &extapi.JSON{Raw: []byte(cfg)},
		ResolvedZone:      "example.com.",
		ResolvedFQDN:      "_acme-challenge.example.com.",
		ResourceNamespace: "default",
		Key:               "the-key",
	}

	err := s.CleanUp(ch)
	if err == nil {
		t.Fatal("expected aggregated error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "delete record 7") {
		t.Errorf("error %q should mention failed record 7", msg)
	}
	if !strings.Contains(msg, "delete record 9") {
		t.Errorf("error %q should mention failed record 9", msg)
	}
	// Record 8 should have succeeded silently — not in error.
	if strings.Contains(msg, "delete record 8") {
		t.Errorf("error %q should not mention successful record 8", msg)
	}
}

func TestDefaultTTLMeetsAPIMinimum(t *testing.T) {
	// The VegaSystems API rejects TTLs below 300 seconds. Any default we ship
	// must clear that floor or every Present-with-default-config call breaks.
	const apiMinimumTTL = 300
	if defaultTTL < apiMinimumTTL {
		t.Errorf("defaultTTL = %d, must be >= %d (VegaSystems API minimum)", defaultTTL, apiMinimumTTL)
	}
}

func TestLoadConfigRejectsNonPositiveCustomerID(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"zero", `{"customerId":0,"apiUserSecretRef":{"name":"s","key":"u"},"apiKeySecretRef":{"name":"s","key":"k"}}`},
		{"negative", `{"customerId":-1,"apiUserSecretRef":{"name":"s","key":"u"},"apiKeySecretRef":{"name":"s","key":"k"}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &solver{}
			ch := &v1alpha1.ChallengeRequest{
				Config:            &extapi.JSON{Raw: []byte(tc.body)},
				ResolvedZone:      "example.com.",
				ResolvedFQDN:      "_acme-challenge.example.com.",
				ResourceNamespace: "default",
			}
			_, _, _, _, err := s.prepare(t.Context(), ch)
			if err == nil {
				t.Fatalf("expected error for non-positive customerId, got nil")
			}
			if !strings.Contains(err.Error(), "customerId") {
				t.Errorf("error %q should mention customerId", err)
			}
		})
	}
}
