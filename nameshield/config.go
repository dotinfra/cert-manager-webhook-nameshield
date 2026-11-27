package nameshield

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

// solverConfig is the configuration passed by cert-manager through the
// Issuer's `webhook.config` field. Secrets must be referenced, never inlined.
type solverConfig struct {
	// APIURL is the Nameshield DNS API base URL.
	// Defaults to https://api.nameshield.net/dns/v4
	APIURL string `json:"apiUrl,omitempty"`

	// APITokenSecretRef references the Secret containing the Nameshield API
	// token. The Secret is looked up in the challenge's resource namespace
	// unless Namespace is set.
	APITokenSecretRef *secretKeySelector `json:"apiTokenSecretRef,omitempty"`

	// ZoneName forces the Nameshield zone to update. When empty, the solver
	// uses the zone resolved by cert-manager (ResolvedZone) or discovers it.
	ZoneName string `json:"zoneName,omitempty"`

	// TTL is the TTL applied to created TXT records. The provider default is
	// used when unset.
	TTL int `json:"ttl,omitempty"`

	// IncludeOtherUsersChanges makes the zone validation also apply pending
	// changes made by other users or tokens. Defaults to false.
	IncludeOtherUsersChanges bool `json:"includeOtherUsersChanges,omitempty"`

	// AllowedDomains restricts the DNS names this solver acts on. When
	// non-empty, a challenge is processed only if its DNS name equals or is
	// a subdomain of one of the entries; other challenges fail fast.
	AllowedDomains []string `json:"allowedDomains,omitempty"`

	// ExcludedDomains lists DNS names this solver must never act on. A
	// challenge whose DNS name equals or is a subdomain of an entry is
	// rejected, even if it also matches AllowedDomains.
	ExcludedDomains []string `json:"excludedDomains,omitempty"`
}

// secretKeySelector references a key of a Kubernetes Secret.
type secretKeySelector struct {
	Name      string `json:"name"`
	Key       string `json:"key"`
	Namespace string `json:"namespace,omitempty"`
}

const ambientTokenEnvVar = "NAMESHIELD_TOKEN"

// parseConfig decodes the challenge configuration JSON.
func parseConfig(cfgJSON *apiextensionsv1.JSON) (solverConfig, error) {
	cfg := solverConfig{}
	if cfgJSON == nil || len(cfgJSON.Raw) == 0 || strings.TrimSpace(string(cfgJSON.Raw)) == "null" {
		return cfg, nil
	}
	if err := json.Unmarshal(cfgJSON.Raw, &cfg); err != nil {
		return cfg, fmt.Errorf("nameshield: error decoding solver config: %w", err)
	}
	return cfg, nil
}

// ambientToken returns the token from the environment when ambient
// credentials are allowed. It is primarily used by the DNS01 conformance
// test suite.
func ambientToken(allowAmbient bool) string {
	if !allowAmbient {
		return ""
	}
	return os.Getenv(ambientTokenEnvVar)
}
