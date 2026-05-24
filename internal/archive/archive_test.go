package archive

import "testing"

func TestSanitize(t *testing.T) {
	got := sanitize("../bad name?.pdf")
	if got != "bad_name_.pdf" {
		t.Fatalf("sanitize() = %q", got)
	}
}
