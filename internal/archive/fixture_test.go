package archive

import "testing"

func TestWriteFixtureVerifies(t *testing.T) {
	dir := t.TempDir()
	if _, err := WriteFixture(dir); err != nil {
		t.Fatal(err)
	}
	manifest, err := Verify(dir)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Totals.Records != 2 || manifest.Totals.Attachments != 1 {
		t.Fatalf("unexpected totals: %+v", manifest.Totals)
	}
}
