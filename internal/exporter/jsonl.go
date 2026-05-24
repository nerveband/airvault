package exporter

import (
	"context"
	"io"
	"os"
	"path/filepath"
)

type JSONL struct{}

func (JSONL) Name() string { return "jsonl" }

func (e JSONL) Plan(ctx context.Context, opts Options) (*Result, error) {
	return e.result(opts)
}

func (e JSONL) Export(ctx context.Context, opts Options) (*Result, error) {
	result, err := e.result(opts)
	if err != nil {
		return nil, err
	}
	if opts.Out == "" {
		return result, nil
	}
	for _, src := range result.Outputs {
		rel, _ := filepath.Rel(opts.ArchivePath, src)
		dst := filepath.Join(opts.Out, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			return nil, err
		}
		if err := copyFile(dst, src, opts.Overwrite); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (JSONL) result(opts Options) (*Result, error) {
	var outputs []string
	err := filepath.WalkDir(opts.ArchivePath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && filepath.Ext(path) == ".jsonl" {
			outputs = append(outputs, path)
		}
		return nil
	})
	return &Result{Exporter: "jsonl", Outputs: outputs}, err
}

func copyFile(dst, src string, overwrite bool) error {
	if !overwrite {
		if _, err := os.Stat(dst); err == nil {
			return os.ErrExist
		}
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func init() { Register(JSONL{}) }
