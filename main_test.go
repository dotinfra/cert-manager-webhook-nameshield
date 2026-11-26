package main

import (
	"os"
	"testing"

	acmetest "github.com/cert-manager/cert-manager/test/acme"

	"github.com/dotinfra/cert-manager-webhook-nameshield/nameshield"
)

// TestRunsSuite runs the cert-manager DNS01 conformance test suite against
// the Nameshield webhook. It is skipped unless both environment variables
// are set:
//
//	NAMESHIELD_ZONE  a real zone managed by Nameshield, e.g. example.com
//	NAMESHIELD_TOKEN a Nameshield API token with DNS rights on that zone
//
// The envtest binaries are required; `make test` downloads them automatically.
func TestRunsSuite(t *testing.T) {
	zone := os.Getenv("NAMESHIELD_ZONE")
	token := os.Getenv("NAMESHIELD_TOKEN")
	if zone == "" || token == "" {
		t.Skip("NAMESHIELD_ZONE and NAMESHIELD_TOKEN must be set to run the conformance test suite")
	}

	fixture := acmetest.NewFixture(nameshield.New(),
		acmetest.SetResolvedZone(zone),
		acmetest.SetManifestPath("testdata/nameshield"),
		acmetest.SetAllowAmbientCredentials(true),
		acmetest.SetUseAuthoritative(true),
	)

	fixture.RunConformance(t)
}
