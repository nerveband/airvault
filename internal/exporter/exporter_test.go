package exporter

import (
	"context"
	"testing"
)

type fakeExporter struct{}

func (fakeExporter) Name() string { return "fake" }
func (fakeExporter) Plan(context.Context, Options) (*Result, error) {
	return &Result{Exporter: "fake"}, nil
}
func (fakeExporter) Export(context.Context, Options) (*Result, error) {
	return &Result{Exporter: "fake"}, nil
}

func TestRegistry(t *testing.T) {
	Register(fakeExporter{})
	if _, err := Get("fake"); err != nil {
		t.Fatal(err)
	}
	if _, err := Get("missing"); err == nil {
		t.Fatal("expected unknown exporter error")
	}
}
