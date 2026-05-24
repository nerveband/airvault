package config

import "testing"

func TestRedacted(t *testing.T) {
	s := &Store{Profiles: map[string]Profile{"p": {Name: "p", Token: "secret", TokenEnv: "AIRTABLE_TOKEN"}}}
	got := s.Redacted().Profiles["p"]
	if got.Token != "redacted" || got.TokenEnv != "AIRTABLE_TOKEN" {
		t.Fatalf("unexpected redaction: %+v", got)
	}
}

func TestMergeDefaults(t *testing.T) {
	base := BuiltinDefaults()
	got := MergeDefaults(base, Defaults{BackupRoot: "/tmp/backups", VerifyMode: "sample", SampleSize: 10})
	if got.BackupRoot != "/tmp/backups" || got.VerifyMode != "sample" || got.SampleSize != 10 {
		t.Fatalf("unexpected defaults: %+v", got)
	}
	if len(got.Include) == 0 || len(got.Exporters) == 0 {
		t.Fatalf("expected builtin slices to remain: %+v", got)
	}
}
