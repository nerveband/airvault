package grist

import "testing"

func TestTranslateFormulaFieldReferences(t *testing.T) {
	got := TranslateFormula("{Score} * 2", map[string]string{"Score": "Score"})
	if got.Status != "translated" {
		t.Fatalf("status = %q, want translated: %#v", got.Status, got)
	}
	if got.Output != "$Score * 2" {
		t.Fatalf("output = %q, want %q", got.Output, "$Score * 2")
	}
}

func TestTranslateFormulaWarnsForLossyAirtableFunctions(t *testing.T) {
	got := TranslateFormula("DATETIME_DIFF({End}, {Start}, 'days')", map[string]string{"End": "End", "Start": "Start"})
	if got.Status != "needs_review" {
		t.Fatalf("status = %q, want needs_review", got.Status)
	}
	if len(got.Warnings) == 0 {
		t.Fatal("expected warning")
	}
}
