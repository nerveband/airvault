package baserow

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

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
			if payload.Name != "Fixture Table" || len(payload.Data) != 3 || payload.Data[1][0] != "recFixture1" {
				t.Fatalf("unexpected payload: %+v", payload)
			}
			writeJSON(t, w, map[string]any{"id": 11})
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
