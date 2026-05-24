package output

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type Delivery struct {
	Scheme string `json:"scheme"`
	Target string `json:"target"`
}

func ParseDelivery(raw string) (Delivery, error) {
	if raw == "" || raw == "stdout" {
		return Delivery{Scheme: "stdout"}, nil
	}
	if strings.HasPrefix(raw, "file:") {
		target := strings.TrimPrefix(raw, "file:")
		if strings.TrimSpace(target) == "" {
			return Delivery{}, fmt.Errorf("file delivery requires a path")
		}
		return Delivery{Scheme: "file", Target: target}, nil
	}
	return Delivery{}, fmt.Errorf("unsupported delivery %q; supported: stdout, file:<path>", raw)
}

func Deliver(raw string, overwrite bool, write func(io.Writer) error) error {
	d, err := ParseDelivery(raw)
	if err != nil {
		return err
	}
	if d.Scheme == "stdout" {
		return write(os.Stdout)
	}
	if !overwrite {
		if _, err := os.Stat(d.Target); err == nil {
			return fmt.Errorf("%s already exists; pass --overwrite to replace it", d.Target)
		}
	}
	if err := os.MkdirAll(filepath.Dir(d.Target), 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(d.Target), ".airvault-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := write(tmp); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, d.Target)
}
