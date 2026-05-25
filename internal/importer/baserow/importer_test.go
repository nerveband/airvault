package baserow

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nerveband/airvault/internal/airtable"
	"github.com/nerveband/airvault/internal/archive"
)

func TestImportDryRunUsesArchiveCounts(t *testing.T) {
	dir := fixtureArchive(t)
	report, err := Import(context.Background(), Options{ArchivePath: dir, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if !report.OK || !report.DryRun || !report.OfflineCapable {
		t.Fatalf("unexpected report flags: %+v", report)
	}
	if report.Counts["bases"] != 1 || report.Counts["tables"] != 1 || report.Counts["records"] != 2 {
		t.Fatalf("unexpected counts: %+v", report.Counts)
	}
}

func TestImportCreatesDatabaseAndTable(t *testing.T) {
	dir := fixtureArchive(t)
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		if r.Header.Get("Authorization") != "JWT test-token" {
			t.Fatalf("missing auth header")
		}
		switch r.URL.Path {
		case "/api/applications/workspace/7/":
			writeJSON(t, w, map[string]any{"id": 10})
		case "/api/database/tables/database/10/":
			var payload struct {
				Name string  `json:"name"`
				Data [][]any `json:"data"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if payload.Name != "Fixture Table" || len(payload.Data) != 1 || payload.Data[0][0] != "Airtable Record ID" {
				t.Fatalf("unexpected payload: %+v", payload)
			}
			writeJSON(t, w, map[string]any{"id": 11})
		case "/api/database/fields/table/11/":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if payload["name"] == "Score" && payload["type"] != "number" {
				t.Fatalf("Score should be number: %+v", payload)
			}
			writeJSON(t, w, map[string]any{"id": 12})
		case "/api/database/rows/table/11/batch/":
			if r.URL.Query().Get("user_field_names") != "true" {
				t.Fatalf("missing user_field_names=true")
			}
			var payload struct {
				Items []map[string]any `json:"items"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if len(payload.Items) != 2 || payload.Items[0]["Airtable Record ID"] != "recFixture1" {
				t.Fatalf("unexpected rows: %+v", payload)
			}
			writeJSON(t, w, map[string]any{"items": []any{}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	report, err := Import(context.Background(), Options{ArchivePath: dir, URL: server.URL, Token: "test-token", WorkspaceID: 7})
	if err != nil {
		t.Fatal(err)
	}
	if report.Counts["records"] != 2 {
		t.Fatalf("unexpected report: %+v", report)
	}
	want := []string{
		"POST /api/applications/workspace/7/",
		"POST /api/database/tables/database/10/",
		"POST /api/database/fields/table/11/",
		"POST /api/database/fields/table/11/",
		"POST /api/database/fields/table/11/",
		"POST /api/database/fields/table/11/",
		"POST /api/database/rows/table/11/batch/",
	}
	if len(paths) != len(want) {
		t.Fatalf("paths = %+v, want %+v", paths, want)
	}
	for i := range want {
		if paths[i] != want[i] {
			t.Fatalf("paths = %+v, want %+v", paths, want)
		}
	}
}

func TestBaserowTypeMapping(t *testing.T) {
	tests := map[string]string{
		"singleLineText":      "text",
		"multilineText":       "long_text",
		"richText":            "long_text",
		"url":                 "url",
		"email":               "email",
		"phoneNumber":         "phone_number",
		"number":              "number",
		"currency":            "number",
		"percent":             "number",
		"rating":              "rating",
		"checkbox":            "boolean",
		"date":                "date",
		"dateTime":            "date",
		"duration":            "duration",
		"singleSelect":        "single_select",
		"multipleSelects":     "multiple_select",
		"multipleAttachments": "file",
	}
	for airtableType, want := range tests {
		if got := baserowType(airtable.Field{Type: airtableType}); got != want {
			t.Fatalf("%s mapped to %s, want %s", airtableType, got, want)
		}
	}
}

func TestBaserowFieldPayloadIncludesSelectOptions(t *testing.T) {
	field := airtable.Field{Name: "Status", Type: "singleSelect", Options: map[string]any{"choices": []any{map[string]any{"name": "Todo"}}}}
	payload := baserowFieldPayload(field, field.Name)
	if payload["type"] != "single_select" || !strings.Contains(commonJSON(payload["select_options"]), "Todo") {
		t.Fatalf("unexpected select payload: %+v", payload)
	}
}

func TestImportRequiresWorkspaceForLiveRun(t *testing.T) {
	dir := fixtureArchive(t)
	_, err := Import(context.Background(), Options{ArchivePath: dir, URL: "http://example.test", Token: "token"})
	if err == nil {
		t.Fatal("expected workspace validation error")
	}
}

func fixtureArchive(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "archive")
	if _, err := archive.WriteFixture(dir); err != nil {
		t.Fatal(err)
	}
	return dir
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Fatal(err)
	}
}

func commonJSON(v any) string {
	data, _ := json.Marshal(v)
	return string(data)
}
