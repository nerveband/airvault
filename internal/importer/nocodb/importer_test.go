package nocodb

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
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

func TestImportCreatesBaseTableAndRows(t *testing.T) {
	dir := fixtureArchive(t)
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		if r.Header.Get("xc-auth") != "test-token" {
			t.Fatalf("missing auth header")
		}
		switch r.URL.Path {
		case "/api/v2/meta/bases":
			writeJSON(t, w, map[string]any{"id": "base1"})
		case "/api/v2/meta/bases/base1/tables":
			writeJSON(t, w, map[string]any{"id": "table1"})
		case "/api/v2/tables/table1/records":
			var rows []map[string]any
			if err := json.NewDecoder(r.Body).Decode(&rows); err != nil {
				t.Fatal(err)
			}
			if len(rows) != 2 || rows[0]["Airtable Record ID"] != "recFixture1" {
				t.Fatalf("unexpected rows: %+v", rows)
			}
			writeJSON(t, w, map[string]any{"ok": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	report, err := Import(context.Background(), Options{ArchivePath: dir, URL: server.URL, Token: "test-token"})
	if err != nil {
		t.Fatal(err)
	}
	if report.Counts["records"] != 2 {
		t.Fatalf("unexpected report: %+v", report)
	}
	want := []string{
		"POST /api/v2/meta/bases",
		"POST /api/v2/meta/bases/base1/tables",
		"POST /api/v2/tables/table1/records",
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

func TestImportRequiresTokenForLiveRun(t *testing.T) {
	dir := fixtureArchive(t)
	_, err := Import(context.Background(), Options{ArchivePath: dir, URL: "http://example.test"})
	if err == nil {
		t.Fatal("expected token validation error")
	}
}

func TestNocoNameRemovesUnsupportedCharacters(t *testing.T) {
	got := nocoName("Ops / CRM: 2026 + Airtable!")
	want := "Ops CRM 2026 Airtable"
	if got != want {
		t.Fatalf("nocoName() = %q, want %q", got, want)
	}
}

func TestNocoFieldNamesAvoidReservedAndDuplicateNames(t *testing.T) {
	table := fixtureTable("tbl", "ID", "ID", "CreatedAt", "Created At", "Created By")
	got := nocoFieldNames(table)
	if got["fld0"] != "ID Field" || got["fld1"] != "ID Field 2" || got["fld2"] != "CreatedAt Field" || got["fld3"] != "Created At Field" || got["fld4"] != "Created By Field" {
		t.Fatalf("unexpected names: %+v", got)
	}
}

func TestNocoTypeMapping(t *testing.T) {
	tests := map[string]string{
		"singleLineText":      "SingleLineText",
		"multilineText":       "LongText",
		"richText":            "LongText",
		"url":                 "URL",
		"email":               "Email",
		"phoneNumber":         "PhoneNumber",
		"number":              "Number",
		"currency":            "Currency",
		"percent":             "Percent",
		"rating":              "Rating",
		"checkbox":            "Checkbox",
		"date":                "Date",
		"dateTime":            "DateTime",
		"duration":            "Duration",
		"singleSelect":        "SingleSelect",
		"multipleSelects":     "MultiSelect",
		"multipleAttachments": "Attachment",
	}
	for airtableType, want := range tests {
		if got := nocoType(airtable.Field{Type: airtableType}); got != want {
			t.Fatalf("%s mapped to %s, want %s", airtableType, got, want)
		}
	}
}

func TestNocoColumnIncludesSelectOptions(t *testing.T) {
	field := airtable.Field{Name: "Status", Type: "singleSelect", Options: map[string]any{"choices": []any{map[string]any{"name": "Both"}}}}
	column := nocoColumn(field, field.Name)
	encoded, _ := json.Marshal(column["colOptions"])
	if !strings.Contains(string(encoded), "Both") {
		t.Fatalf("expected select option in column: %+v", column)
	}
}

func TestFixtureArchiveExists(t *testing.T) {
	dir := fixtureArchive(t)
	if _, err := os.Stat(filepath.Join(dir, "manifest.json")); err != nil {
		t.Fatal(err)
	}
}

func fixtureTable(id string, names ...string) airtable.Table {
	fields := make([]airtable.Field, 0, len(names))
	for i, name := range names {
		fields = append(fields, airtable.Field{ID: fmt.Sprintf("fld%d", i), Name: name, Type: "singleLineText"})
	}
	return airtable.Table{ID: id, Name: "Test", Fields: fields}
}
