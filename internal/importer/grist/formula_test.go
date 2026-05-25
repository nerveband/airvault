package grist

import (
	"testing"

	"github.com/nerveband/airvault/internal/airtable"
)

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

func TestGristTypeMapping(t *testing.T) {
	tests := map[string]string{
		"singleLineText":      "Text",
		"multilineText":       "Text",
		"url":                 "Text",
		"email":               "Text",
		"phoneNumber":         "Text",
		"number":              "Numeric",
		"currency":            "Numeric",
		"percent":             "Numeric",
		"rating":              "Numeric",
		"checkbox":            "Bool",
		"date":                "Date",
		"dateTime":            "DateTime",
		"duration":            "Numeric",
		"singleSelect":        "Choice",
		"multipleSelects":     "ChoiceList",
		"multipleAttachments": "Attachments",
	}
	for airtableType, want := range tests {
		if got := gristType(airtable.Field{Type: airtableType}); got != want {
			t.Fatalf("%s mapped to %s, want %s", airtableType, got, want)
		}
	}
}
