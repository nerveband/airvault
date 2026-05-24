package exporter

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

type SQLite struct{}

func (SQLite) Name() string { return "sqlite" }

func (e SQLite) Plan(ctx context.Context, opts Options) (*Result, error) {
	out := opts.Out
	if out == "" {
		out = "airvault.sqlite"
	}
	return &Result{Exporter: "sqlite", Outputs: []string{out}}, nil
}

func (e SQLite) Export(ctx context.Context, opts Options) (*Result, error) {
	plan, err := e.Plan(ctx, opts)
	if err != nil {
		return nil, err
	}
	out := plan.Outputs[0]
	if !opts.Overwrite {
		if _, err := os.Stat(out); err == nil {
			return nil, fmt.Errorf("%s exists; pass --overwrite", out)
		}
	}
	if err := os.MkdirAll(filepath.Dir(out), 0755); err != nil && filepath.Dir(out) != "." {
		return nil, err
	}
	db, err := sql.Open("sqlite", out)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `create table if not exists records (base_id text, table_id text, record_id text primary key, payload text not null)`); err != nil {
		return nil, err
	}
	err = filepath.WalkDir(opts.ArchivePath, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Base(path) != "records.jsonl" {
			return err
		}
		tableID := filepath.Base(filepath.Dir(path))
		baseID := baseIDFromPath(opts.ArchivePath, path)
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
			_, err := db.ExecContext(ctx, `insert into records(base_id,table_id,record_id,payload) values(?,?,?,?) on conflict(record_id) do update set payload=excluded.payload`, baseID, tableID, id, scanner.Text())
			if err != nil {
				return err
			}
		}
		return scanner.Err()
	})
	return plan, err
}

func baseIDFromPath(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return ""
	}
	items := strings.Split(filepath.ToSlash(rel), "/")
	if len(items) >= 2 && items[0] == "bases" {
		return items[1]
	}
	return ""
}

func init() { Register(SQLite{}) }
