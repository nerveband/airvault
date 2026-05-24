package config

import "testing"

func TestRedacted(t *testing.T) {
	s := &Store{Profiles: map[string]Profile{"p": {Name: "p", Token: "secret", TokenEnv: "AIRTABLE_TOKEN"}}}
	got := s.Redacted().Profiles["p"]
	if got.Token != "redacted" || got.TokenEnv != "AIRTABLE_TOKEN" {
		t.Fatalf("unexpected redaction: %+v", got)
	}
}
