package archive

import (
	"testing"

	"github.com/nerveband/airvault/internal/airtable"
)

func TestSanitize(t *testing.T) {
	got := sanitize("../bad name?.pdf")
	if got != "bad_name_.pdf" {
		t.Fatalf("sanitize() = %q", got)
	}
}

func TestIncludesTable(t *testing.T) {
	selection := Selection{TableIDsOrNames: []string{"tbl123", "Students"}}
	if !includesTable(selection, airtable.Table{ID: "tbl123", Name: "Other"}) {
		t.Fatal("expected ID match")
	}
	if !includesTable(selection, airtable.Table{ID: "tbl999", Name: "Students"}) {
		t.Fatal("expected name match")
	}
	if includesTable(selection, airtable.Table{ID: "tbl999", Name: "Adults"}) {
		t.Fatal("did not expect unrelated table")
	}
}
