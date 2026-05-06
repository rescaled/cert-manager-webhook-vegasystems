//go:build conformance

package main

import (
	"os"
	"testing"

	acmetest "github.com/cert-manager/cert-manager/test/acme"
)

// TestRunsConformance runs the cert-manager DNS01 conformance suite against
// the live VegaSystems API. It only builds with the `conformance` tag because
// importing the cert-manager acme test package pulls in envtest, which
// requires kube-apiserver/etcd binaries on PATH.
//
// Usage:
//
//	export TEST_ZONE_NAME=example.com.
//	go test -tags conformance -v -run TestRunsConformance .
//
// Place credentials in testdata/vegasystems/secret.yaml and the customer ID
// in testdata/vegasystems/config.json.
func TestRunsConformance(t *testing.T) {
	zone := os.Getenv("TEST_ZONE_NAME")
	if zone == "" {
		t.Fatal("TEST_ZONE_NAME must be set, e.g. example.com.")
	}

	fixture := acmetest.NewFixture(&solver{},
		acmetest.SetResolvedZone(zone),
		acmetest.SetManifestPath("testdata/vegasystems"),
		acmetest.SetStrict(true),
	)
	fixture.RunConformance(t)
}
