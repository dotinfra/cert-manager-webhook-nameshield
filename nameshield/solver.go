package nameshield

import (
	"context"
	"fmt"
	"strings"

	"github.com/cert-manager/cert-manager/pkg/acme/webhook"
	acme "github.com/cert-manager/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	rest "k8s.io/client-go/rest"
)

// SolverName is the solver name referenced in the Issuer's webhook stanza.
const SolverName = "nameshield"

// Solver implements webhook.Solver for the Nameshield DNS API.
type Solver struct {
	// client is used to read Secret resources containing the API token.
	client kubernetes.Interface
}

var _ webhook.Solver = (*Solver)(nil)

// New returns a new Nameshield webhook solver.
func New() *Solver {
	return &Solver{}
}

// Name is used as the name for this DNS solver when referencing it on the
// ACME Issuer resource.
func (s *Solver) Name() string {
	return SolverName
}

// Initialize is called when the webhook first starts. It builds a Kubernetes
// client used to fetch Secret resources referenced by solver configurations.
func (s *Solver) Initialize(kubeClientConfig *rest.Config, stopCh <-chan struct{}) error {
	cl, err := kubernetes.NewForConfig(kubeClientConfig)
	if err != nil {
		return fmt.Errorf("nameshield: failed to build Kubernetes client: %w", err)
	}
	s.client = cl
	return nil
}

// Present creates the TXT record for the ACME dns-01 challenge and applies
// the zone changes, which the Nameshield API keeps pending until validated.
func (s *Solver) Present(ch *acme.ChallengeRequest) error {
	cfg, err := parseConfig(ch.Config)
	if err != nil {
		return err
	}

	if err := checkDomainFilter(cfg, ch); err != nil {
		return err
	}

	ctx := context.Background()

	token, err := s.resolveToken(ctx, ch, cfg)
	if err != nil {
		return err
	}

	api, err := NewClient(cfg.APIURL, token)
	if err != nil {
		return err
	}

	zone, err := resolveZone(ctx, api, ch, cfg)
	if err != nil {
		return err
	}

	name, err := recordName(ch.ResolvedFQDN, zone)
	if err != nil {
		return err
	}

	// Present must tolerate multiple calls with the same arguments.
	// If the record already exists, validate pending changes and return.
	existing, err := api.ListRecords(ctx, zone, name, true)
	if err != nil {
		return fmt.Errorf("nameshield: failed to list zone %q records: %w", zone, err)
	}
	data := quoteTxt(ch.Key)
	if !hasRecordWith(existing, data) {
		if err := api.CreateTxtRecord(ctx, zone, name, data, cfg.TTL); err != nil {
			return fmt.Errorf("nameshield: failed to create TXT record %q in zone %q: %w", name, zone, err)
		}
	}

	// Changes are only applied after zone validation.
	if err := api.ValidateZone(ctx, zone, cfg.IncludeOtherUsersChanges); err != nil {
		return fmt.Errorf("nameshield: failed to validate zone %q: %w", zone, err)
	}

	return nil
}

// CleanUp deletes the TXT records matching the challenge and applies the
// zone changes.
func (s *Solver) CleanUp(ch *acme.ChallengeRequest) error {
	cfg, err := parseConfig(ch.Config)
	if err != nil {
		return err
	}

	if err := checkDomainFilter(cfg, ch); err != nil {
		// Filtered challenges never created a record; nothing to clean up.
		return nil
	}

	ctx := context.Background()

	token, err := s.resolveToken(ctx, ch, cfg)
	if err != nil {
		return err
	}

	api, err := NewClient(cfg.APIURL, token)
	if err != nil {
		return err
	}

	zone, err := resolveZone(ctx, api, ch, cfg)
	if err != nil {
		return err
	}

	name, err := recordName(ch.ResolvedFQDN, zone)
	if err != nil {
		return err
	}

	records, err := api.ListRecords(ctx, zone, name, true)
	if err != nil {
		return fmt.Errorf("nameshield: failed to list zone %q records: %w", zone, err)
	}

	data := quoteTxt(ch.Key)
	deleted := false
	for _, r := range records {
		if recordMatches(r, data) {
			if err := api.DeleteRecord(ctx, zone, r.ID.String()); err != nil {
				return fmt.Errorf("nameshield: failed to delete TXT record %q (id %s) in zone %q: %w", name, r.ID, zone, err)
			}
			deleted = true
		}
	}

	if deleted {
		if err := api.ValidateZone(ctx, zone, cfg.IncludeOtherUsersChanges); err != nil {
			return fmt.Errorf("nameshield: failed to validate zone %q: %w", zone, err)
		}
	}

	return nil
}

// checkDomainFilter enforces the allowedDomains/excludedDomains solver
// config. It returns an error when the challenge's DNS name must not be
// handled by this solver.
func checkDomainFilter(cfg solverConfig, ch *acme.ChallengeRequest) error {
	name := challengeDNSName(ch)
	if domainMatchesAny(name, cfg.ExcludedDomains) {
		return fmt.Errorf("nameshield: dnsName %q is excluded by the solver config (excludedDomains)", name)
	}
	if len(cfg.AllowedDomains) > 0 && !domainMatchesAny(name, cfg.AllowedDomains) {
		return fmt.Errorf("nameshield: dnsName %q is not allowed by the solver config (allowedDomains)", name)
	}
	return nil
}

// challengeDNSName returns the DNS name being validated: ch.DNSName when
// set, otherwise the ResolvedFQDN stripped of its _acme-challenge prefix.
func challengeDNSName(ch *acme.ChallengeRequest) string {
	if name := normalizeDomain(ch.DNSName); name != "" {
		return name
	}
	return strings.TrimPrefix(normalizeDomain(ch.ResolvedFQDN), "_acme-challenge.")
}

// normalizeDomain lowercases a domain name, strips a leading wildcard label
// and a trailing dot so config entries and challenge names compare
// consistently.
func normalizeDomain(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.TrimSuffix(name, ".")
	return strings.TrimPrefix(name, "*.")
}

// domainMatchesAny reports whether name equals an entry or is a subdomain of
// it, e.g. "www.example.com" matches entry "example.com".
func domainMatchesAny(name string, entries []string) bool {
	for _, entry := range entries {
		entry = normalizeDomain(entry)
		if entry == "" {
			continue
		}
		if name == entry || strings.HasSuffix(name, "."+entry) {
			return true
		}
	}
	return false
}

// resolveToken returns the Nameshield API token, from the Secret referenced
// in the solver configuration or from ambient credentials.
func (s *Solver) resolveToken(ctx context.Context, ch *acme.ChallengeRequest, cfg solverConfig) (string, error) {
	if ref := cfg.APITokenSecretRef; ref != nil {
		if ref.Name == "" || ref.Key == "" {
			return "", fmt.Errorf("nameshield: apiTokenSecretRef requires both name and key")
		}
		namespace := ref.Namespace
		if namespace == "" {
			namespace = ch.ResourceNamespace
		}
		if s.client == nil {
			return "", fmt.Errorf("nameshield: Kubernetes client is not initialized")
		}
		secret, err := s.client.CoreV1().Secrets(namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil {
			return "", fmt.Errorf("nameshield: failed to read Secret %s/%s: %w", namespace, ref.Name, err)
		}
		val, ok := secret.Data[ref.Key]
		if !ok {
			return "", fmt.Errorf("nameshield: key %q not found in Secret %s/%s", ref.Key, namespace, ref.Name)
		}
		token := strings.TrimSpace(string(val))
		if token == "" {
			return "", fmt.Errorf("nameshield: Secret %s/%s key %q is empty", namespace, ref.Name, ref.Key)
		}
		return token, nil
	}

	if token := ambientToken(ch.AllowAmbientCredentials); token != "" {
		return token, nil
	}

	return "", fmt.Errorf("nameshield: no API token available: set apiTokenSecretRef in the solver config")
}

// resolveZone determines the Nameshield zone hosting the challenge record:
//  1. explicit zoneName from the solver config,
//  2. the zone resolved by cert-manager, when it is a valid Nameshield zone,
//  3. discovery by probing parent names, most specific first.
func resolveZone(ctx context.Context, api *Client, ch *acme.ChallengeRequest, cfg solverConfig) (string, error) {
	if cfg.ZoneName != "" {
		return trimDot(cfg.ZoneName), nil
	}

	if zone := trimDot(ch.ResolvedZone); zone != "" {
		ok, err := api.ZoneExists(ctx, zone)
		if err != nil {
			return "", err
		}
		if ok {
			return zone, nil
		}
		// ResolvedZone may be a delegated sub-zone that is not managed in
		// Nameshield; fall through to discovery.
	}

	zone, err := discoverZone(ctx, api, trimDot(ch.ResolvedFQDN))
	if err != nil {
		return "", err
	}
	return zone, nil
}

// recordName returns the record name relative to the zone apex. Nameshield
// expects relative names: the apex itself is "" and
// "_acme-challenge.example.com" in zone "example.com" is "_acme-challenge".
func recordName(fqdn, zone string) (string, error) {
	fqdn = trimDot(fqdn)
	zone = trimDot(zone)
	if fqdn == "" || zone == "" {
		return "", fmt.Errorf("nameshield: cannot compute record name from fqdn %q and zone %q", fqdn, zone)
	}
	if strings.HasSuffix(fqdn, "."+zone) {
		return strings.TrimSuffix(fqdn, "."+zone), nil
	}
	if fqdn == zone {
		// Should not happen for _acme-challenge records; treat as apex.
		return "", nil
	}
	return "", fmt.Errorf("nameshield: fqdn %q is not inside zone %q", fqdn, zone)
}

// discoverZone finds the deepest existing zone among the parent names of
// fqdn, skipping the leading label (normally _acme-challenge).
func discoverZone(ctx context.Context, api *Client, fqdn string) (string, error) {
	if fqdn == "" {
		return "", fmt.Errorf("nameshield: empty FQDN, cannot discover zone")
	}
	labels := strings.Split(fqdn, ".")
	candidate := strings.Join(labels[1:], ".")
	for {
		if candidate == "" || !strings.Contains(candidate, ".") {
			break
		}
		ok, err := api.ZoneExists(ctx, candidate)
		if err != nil {
			return "", fmt.Errorf("nameshield: zone discovery failed while testing %q: %w", candidate, err)
		}
		if ok {
			return candidate, nil
		}
		parts := strings.Split(candidate, ".")
		if len(parts) < 3 {
			break
		}
		candidate = strings.Join(parts[1:], ".")
	}
	return "", fmt.Errorf("nameshield: no zone found for %q; specify zoneName in the solver config", fqdn)
}

// quoteTxt wraps a TXT record value in double quotes as required by the
// Nameshield API for TXT data.
func quoteTxt(value string) string {
	return "\"" + value + "\""
}

// recordMatches reports whether a record carries the challenge TXT value,
// accepting both quoted and unquoted data forms.
func recordMatches(r Record, quotedData string) bool {
	if r.Type != "TXT" {
		return false
	}
	if r.Data == quotedData {
		return true
	}
	return strings.Trim(r.Data, "\"") == strings.Trim(quotedData, "\"")
}

// hasRecordWith reports whether any record matches the challenge value.
func hasRecordWith(records []Record, quotedData string) bool {
	for _, r := range records {
		if recordMatches(r, quotedData) {
			return true
		}
	}
	return false
}

func trimDot(s string) string {
	return strings.TrimSuffix(strings.TrimSpace(s), ".")
}
