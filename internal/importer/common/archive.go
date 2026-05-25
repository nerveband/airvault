package common

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/nerveband/airvault/internal/airtable"
	"github.com/nerveband/airvault/internal/archive"
)

func ReadManifest(root string) (*archive.Manifest, error) {
	var m archive.Manifest
	return &m, readJSON(filepath.Join(root, "manifest.json"), &m)
}

func ReadSchema(root, baseID string) (*airtable.Schema, error) {
	var s airtable.Schema
	return &s, readJSON(filepath.Join(root, "bases", baseID, "schema.json"), &s)
}

func ReadRows(root, baseID string, table airtable.Table) ([]airtable.Record, error) {
	file, err := os.Open(filepath.Join(root, "bases", baseID, "tables", table.ID, "records.jsonl"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()
	var out []airtable.Record
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var rec airtable.Record
		if err := json.Unmarshal(scanner.Bytes(), &rec); err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, scanner.Err()
}

func Stringify(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(data)
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}
