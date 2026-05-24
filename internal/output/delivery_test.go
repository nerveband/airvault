package output

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestParseDelivery(t *testing.T) {
	d, err := ParseDelivery("file:/tmp/out.json")
	if err != nil {
		t.Fatal(err)
	}
	if d.Scheme != "file" || d.Target != "/tmp/out.json" {
		t.Fatalf("unexpected delivery: %+v", d)
	}
}

func TestDeliverRefusesOverwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")
	if err := os.WriteFile(path, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	err := Deliver("file:"+path, false, func(w io.Writer) error {
		_, err := w.Write([]byte("new"))
		return err
	})
	if err == nil {
		t.Fatal("expected overwrite refusal")
	}
}
