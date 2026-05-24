package exporter

import (
	"context"
	"fmt"
	"sort"
)

type Options struct {
	ArchivePath string `json:"archive_path"`
	Out         string `json:"out"`
	Deliver     string `json:"deliver,omitempty"`
	Overwrite   bool   `json:"overwrite"`
}

type Result struct {
	Exporter string   `json:"exporter"`
	Outputs  []string `json:"outputs"`
}

type Exporter interface {
	Name() string
	Plan(context.Context, Options) (*Result, error)
	Export(context.Context, Options) (*Result, error)
}

var registry = map[string]Exporter{}

func Register(e Exporter) {
	registry[e.Name()] = e
}

func Get(name string) (Exporter, error) {
	e, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown exporter %q; supported: %v", name, Names())
	}
	return e, nil
}

func Names() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
