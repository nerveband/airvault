package exporter

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Postgres struct{}

func (Postgres) Name() string { return "postgres" }

func (e Postgres) Plan(ctx context.Context, opts Options) (*Result, error) {
	out := opts.Out
	if out == "" {
		out = "airvault.sql"
	}
	return &Result{Exporter: "postgres", Outputs: []string{out}}, nil
}

func (e Postgres) Export(ctx context.Context, opts Options) (*Result, error) {
	plan, err := e.Plan(ctx, opts)
	if err != nil {
		return nil, err
	}
	outPath := plan.Outputs[0]
	if !opts.Overwrite {
		if _, err := os.Stat(outPath); err == nil {
			return nil, fmt.Errorf("%s exists; pass --overwrite", outPath)
		}
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil && filepath.Dir(outPath) != "." {
		return nil, err
	}
	out, err := os.Create(outPath)
	if err != nil {
		return nil, err
	}
	defer out.Close()
	fmt.Fprintln(out, "create schema if not exists airvault;")
	err = filepath.WalkDir(opts.ArchivePath, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Base(path) != "records.jsonl" {
			return err
		}
		tableID := filepath.Base(filepath.Dir(path))
		tableName := "airvault_" + safeIdent(tableID)
		fmt.Fprintf(out, "create table if not exists airvault.%s (record_id text primary key, payload jsonb not null);\n", tableName)
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			var rec map[string]any
			if err := json.Unmarshal(scanner.Bytes(), &rec); err != nil {
				return err
			}
			id, _ := rec["id"].(string)
			payload, _ := json.Marshal(rec)
			fmt.Fprintf(out, "insert into airvault.%s (record_id,payload) values ('%s','%s'::jsonb) on conflict (record_id) do update set payload=excluded.payload;\n", tableName, strings.ReplaceAll(id, "'", "''"), strings.ReplaceAll(string(payload), "'", "''"))
		}
		return scanner.Err()
	})
	return plan, err
}

func safeIdent(s string) string {
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			return r
		}
		return '_'
	}, s)
}

func init() { Register(Postgres{}) }
