package airtable

import "testing"

func TestBaseIDFromEndpoint(t *testing.T) {
	cases := map[string]string{
		"https://api.airtable.com/v0/app123/Table?pageSize=100":     "app123",
		"https://api.airtable.com/v0/meta/bases/app456/tables":      "app456",
		"https://api.airtable.com/v0/meta/bases":                    "",
		"https://api.airtable.com/v0/app789/tbl123/rec123/comments": "app789",
	}
	for endpoint, want := range cases {
		if got := baseIDFromEndpoint(endpoint); got != want {
			t.Fatalf("baseIDFromEndpoint(%q) = %q, want %q", endpoint, got, want)
		}
	}
}

func TestTelemetryRecordsRateLimit(t *testing.T) {
	r := newTelemetryRecorder()
	r.observe("app123", "https://api.airtable.com/v0/app123/Table", 429)
	r.observeRateLimit("app123", "https://api.airtable.com/v0/app123/Table", 100)
	got := r.snapshot()
	if got.RateLimitResponses != 1 || got.ByBase["app123"].RateLimitResponses != 1 {
		t.Fatalf("unexpected telemetry: %+v", got)
	}
	if len(got.Restrictions) != 1 || got.Restrictions[0].Code != "RATE_LIMITED" {
		t.Fatalf("missing restriction: %+v", got.Restrictions)
	}
}
