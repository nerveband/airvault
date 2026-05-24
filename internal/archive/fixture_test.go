package archive

import "testing"

func TestWriteFixtureVerifies(t *testing.T) {
	dir := t.TempDir()
	if _, err := WriteFixture(dir); err != nil {
		t.Fatal(err)
	}
	report, err := VerifyWithOptions(dir, VerifyOptions{Mode: VerifyExists})
	if err != nil {
		t.Fatal(err)
	}
	if !report.OK || !report.Checks.Ledger || !report.Checks.AttachmentPath || !report.Checks.AttachmentSize {
		t.Fatalf("unexpected report: %+v", report)
	}
	if report.Totals.Records != 2 || report.Totals.Attachments != 1 {
		t.Fatalf("unexpected totals: %+v", report.Totals)
	}
}

func TestVerifySample(t *testing.T) {
	dir := t.TempDir()
	if _, err := WriteFixture(dir); err != nil {
		t.Fatal(err)
	}
	report, err := VerifyWithOptions(dir, VerifyOptions{Mode: VerifySample, SampleSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	if report.Sampled != 1 || !report.Checks.Checksums {
		t.Fatalf("unexpected sample report: %+v", report)
	}
}
