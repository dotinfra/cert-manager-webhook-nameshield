package nameshield

import (
	"testing"
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
