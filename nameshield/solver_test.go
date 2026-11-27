package nameshield

import (
	"testing"

	acme "github.com/cert-manager/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

func TestQuoteTxt(t *testing.T) {
	if got, want := quoteTxt("abc"), `"abc"`; got != want {
		t.Errorf("quoteTxt = %q, want %q", got, want)
	}
}

func TestRecordMatches(t *testing.T) {
	quoted := `"KucXEVroidRBeGAFsaDY0m3b9S14Mi-4WfRdAk4p"`
	tests := []struct {
		desc     string
		record   Record
		quotedIn string
		want     bool
	}{
		{
			desc:     "quoted data",
			record:   Record{Type: "TXT", Data: quoted},
			quotedIn: quoted,
			want:     true,
		},
		{
			desc:     "unquoted data",
			record:   Record{Type: "TXT", Data: `KucXEVroidRBeGAFsaDY0m3b9S14Mi-4WfRdAk4p`},
			quotedIn: quoted,
			want:     true,
		},
		{
			desc:     "wrong value",
			record:   Record{Type: "TXT", Data: `"other"`},
			quotedIn: quoted,
			want:     false,
		},
		{
			desc:     "wrong type",
			record:   Record{Type: "A", Data: quoted},
			quotedIn: quoted,
			want:     false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if got := recordMatches(tt.record, tt.quotedIn); got != tt.want {
				t.Errorf("recordMatches = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRecordName(t *testing.T) {
	tests := []struct {
		desc    string
		fqdn    string
		zone    string
		want    string
		wantErr bool
	}{
		{
			desc: "apex challenge",
			fqdn: "_acme-challenge.example.com.",
			zone: "example.com.",
			want: "_acme-challenge",
		},
		{
			desc: "subdomain challenge",
			fqdn: "_acme-challenge.sub.example.com.",
			zone: "example.com.",
			want: "_acme-challenge.sub",
		},
		{
			desc: "equal fqdn and zone",
			fqdn: "example.com.",
			zone: "example.com.",
			want: "",
		},
		{
			desc:    "outside zone",
			fqdn:    "_acme-challenge.example.org.",
			zone:    "example.com.",
			wantErr: true,
		},
		{
			desc:    "empty inputs",
			fqdn:    "",
			zone:    "",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			got, err := recordName(tt.fqdn, tt.zone)
			if (err != nil) != tt.wantErr {
				t.Fatalf("recordName error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && got != tt.want {
				t.Errorf("recordName = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseConfig(t *testing.T) {
	cfg, err := parseConfig(nil)
	if err != nil {
		t.Fatalf("parseConfig(nil) error: %v", err)
	}
	if cfg.APIURL != "" || cfg.APITokenSecretRef != nil {
		t.Errorf("parseConfig(nil) = %+v, want empty config", cfg)
	}
}

func TestParseConfigDomainFilter(t *testing.T) {
	raw := &apiextensionsv1.JSON{Raw: []byte(`{
		"allowedDomains": ["Example.COM.", "*.corp.example.org"],
		"excludedDomains": ["www.example.com"]
	}`)}
	cfg, err := parseConfig(raw)
	if err != nil {
		t.Fatalf("parseConfig error: %v", err)
	}
	if len(cfg.AllowedDomains) != 2 || cfg.AllowedDomains[0] != "Example.COM." || cfg.AllowedDomains[1] != "*.corp.example.org" {
		t.Errorf("AllowedDomains = %v, want [Example.COM. *.corp.example.org]", cfg.AllowedDomains)
	}
	if len(cfg.ExcludedDomains) != 1 || cfg.ExcludedDomains[0] != "www.example.com" {
		t.Errorf("ExcludedDomains = %v, want [www.example.com]", cfg.ExcludedDomains)
	}
}

func TestDomainMatchesAny(t *testing.T) {
	tests := []struct {
		desc    string
		name    string
		entries []string
		want    bool
	}{
		{desc: "exact match", name: "example.com", entries: []string{"example.com"}, want: true},
		{desc: "subdomain match", name: "www.example.com", entries: []string{"example.com"}, want: true},
		{desc: "deep subdomain match", name: "a.b.example.com", entries: []string{"example.com"}, want: true},
		{desc: "wildcard name matches apex entry", name: "*.example.com", entries: []string{"example.com"}, want: true},
		{desc: "trailing dot and case normalized", name: "WWW.Example.COM.", entries: []string{"example.com"}, want: true},
		{desc: "entry with trailing dot and wildcard", name: "corp.example.org", entries: []string{"*.corp.example.org."}, want: true},
		{desc: "no match", name: "example.org", entries: []string{"example.com"}, want: false},
		{desc: "suffix is not a domain boundary", name: "notexample.com", entries: []string{"example.com"}, want: false},
		{desc: "parent does not match child entry", name: "example.com", entries: []string{"www.example.com"}, want: false},
		{desc: "empty entry ignored", name: "example.com", entries: []string{"", "  "}, want: false},
		{desc: "empty entries", name: "example.com", entries: nil, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if got := domainMatchesAny(normalizeDomain(tt.name), tt.entries); got != tt.want {
				t.Errorf("domainMatchesAny(%q, %v) = %v, want %v", tt.name, tt.entries, got, tt.want)
			}
		})
	}
}

func TestCheckDomainFilter(t *testing.T) {
	tests := []struct {
		desc    string
		cfg     solverConfig
		ch      *acme.ChallengeRequest
		wantErr bool
	}{
		{
			desc: "no filter configured allows everything",
			ch:   &acme.ChallengeRequest{DNSName: "anything.example.net"},
		},
		{
			desc:    "allowedDomains allows matching name",
			cfg:     solverConfig{AllowedDomains: []string{"example.com"}},
			ch:      &acme.ChallengeRequest{DNSName: "www.example.com"},
			wantErr: false,
		},
		{
			desc:    "allowedDomains rejects other domain",
			cfg:     solverConfig{AllowedDomains: []string{"example.com"}},
			ch:      &acme.ChallengeRequest{DNSName: "example.org"},
			wantErr: true,
		},
		{
			desc:    "wildcard challenge allowed by apex entry",
			cfg:     solverConfig{AllowedDomains: []string{"example.com"}},
			ch:      &acme.ChallengeRequest{DNSName: "*.example.com"},
			wantErr: false,
		},
		{
			desc:    "excludedDomains rejects listed subdomain",
			cfg:     solverConfig{ExcludedDomains: []string{"www.example.com"}},
			ch:      &acme.ChallengeRequest{DNSName: "www.example.com"},
			wantErr: true,
		},
		{
			desc:    "excludedDomains rejects subdomain of listed entry",
			cfg:     solverConfig{ExcludedDomains: []string{"www.example.com"}},
			ch:      &acme.ChallengeRequest{DNSName: "api.www.example.com"},
			wantErr: true,
		},
		{
			desc:    "exclusion wins over allowlist",
			cfg:     solverConfig{AllowedDomains: []string{"example.com"}, ExcludedDomains: []string{"www.example.com"}},
			ch:      &acme.ChallengeRequest{DNSName: "www.example.com"},
			wantErr: true,
		},
		{
			desc:    "non-excluded sibling allowed",
			cfg:     solverConfig{AllowedDomains: []string{"example.com"}, ExcludedDomains: []string{"www.example.com"}},
			ch:      &acme.ChallengeRequest{DNSName: "api.example.com"},
			wantErr: false,
		},
		{
			desc:    "falls back to ResolvedFQDN when DNSName is empty",
			cfg:     solverConfig{AllowedDomains: []string{"example.com"}},
			ch:      &acme.ChallengeRequest{ResolvedFQDN: "_acme-challenge.example.org."},
			wantErr: true,
		},
		{
			desc:    "ResolvedFQDN fallback matches allowed zone",
			cfg:     solverConfig{AllowedDomains: []string{"example.com"}},
			ch:      &acme.ChallengeRequest{ResolvedFQDN: "_acme-challenge.www.example.com."},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			err := checkDomainFilter(tt.cfg, tt.ch)
			if (err != nil) != tt.wantErr {
				t.Errorf("checkDomainFilter() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
