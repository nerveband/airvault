package nocodb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/nerveband/airvault/internal/airtable"
	"github.com/nerveband/airvault/internal/importer/common"
)

type Options struct {
	ArchivePath string
	URL         string
	Token       string
	DryRun      bool
	BatchSize   int
}

type Report struct {
	OK             bool           `json:"ok"`
	DryRun         bool           `json:"dry_run"`
	OfflineCapable bool           `json:"offline_capable"`
	TargetURL      string         `json:"target_url,omitempty"`
	Counts         map[string]int `json:"counts"`
	Bases          []BaseReport   `json:"bases"`
	Warnings       []string       `json:"warnings,omitempty"`
}

type BaseReport struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	TargetID string `json:"target_id,omitempty"`
	Tables   int    `json:"tables"`
	Rows     int    `json:"rows"`
}

type Client struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
}

func Import(ctx context.Context, opts Options) (*Report, error) {
	if opts.ArchivePath == "" {
		return nil, fmt.Errorf("archive path is required")
	}
	if !opts.DryRun && opts.URL == "" {
		return nil, fmt.Errorf("nocodb url is required unless --dry-run is set")
	}
	if !opts.DryRun && opts.Token == "" {
		return nil, fmt.Errorf("nocodb token is required unless --dry-run is set")
	}
	if opts.BatchSize <= 0 {
		opts.BatchSize = 100
	}
	manifest, err := common.ReadManifest(opts.ArchivePath)
	if err != nil {
		return nil, err
	}
	report := &Report{
		OK: true, DryRun: opts.DryRun, OfflineCapable: true, TargetURL: opts.URL,
		Counts: map[string]int{"bases": 0, "tables": 0, "records": 0},
		Warnings: []string{
			"NocoDB is offline-capable when self-hosted with local Docker volumes or an external local database",
			"Airtable views, formulas, interfaces, automations, comments, and exact attachment fields are not one-to-one portable",
			"Complex Airtable values are stored as JSON text for portability",
		},
	}
	client := NewClient(opts.URL, opts.Token)
	for _, base := range manifest.Bases {
		schema, err := common.ReadSchema(opts.ArchivePath, base.ID)
		if err != nil {
			return nil, err
		}
		targetID := "dry_" + base.ID
		if !opts.DryRun {
			targetID, err = client.CreateBase(ctx, "Airvault - "+nocoName(base.Name))
			if err != nil {
				return nil, err
			}
		}
		br := BaseReport{ID: base.ID, Name: base.Name, TargetID: targetID}
		report.Counts["bases"]++
		for _, table := range schema.Tables {
			rows, err := common.ReadRows(opts.ArchivePath, base.ID, table)
			if err != nil {
				return nil, err
			}
			tableID := "dry_" + table.ID
			if !opts.DryRun {
				tableID, err = client.CreateTable(ctx, targetID, table)
				if err != nil {
					return nil, err
				}
				if err := client.InsertRowsBatched(ctx, tableID, convertRows(table, rows), opts.BatchSize); err != nil {
					return nil, err
				}
			}
			_ = tableID
			br.Tables++
			br.Rows += len(rows)
			report.Counts["tables"]++
			report.Counts["records"] += len(rows)
		}
		report.Bases = append(report.Bases, br)
	}
	return report, nil
}

var nocoNameUnsafe = regexp.MustCompile(`[^A-Za-z0-9 _.\-()&,']+`)

func nocoName(name string) string {
	out := nocoNameUnsafe.ReplaceAllString(strings.TrimSpace(name), " ")
	out = strings.Join(strings.Fields(out), " ")
	if out == "" {
		return "Imported"
	}
	return out
}

func NewClient(baseURL, token string) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), Token: token, HTTPClient: &http.Client{Timeout: 120 * time.Second}}
}

func (c *Client) CreateBase(ctx context.Context, name string) (string, error) {
	var out struct {
		ID string `json:"id"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/api/v2/meta/bases", map[string]any{"title": name, "type": "database"}, &out); err != nil {
		return "", err
	}
	return out.ID, nil
}

func (c *Client) CreateTable(ctx context.Context, baseID string, table airtable.Table) (string, error) {
	columns := []map[string]any{{"title": "Airtable Record ID", "column_name": "Airtable_Record_ID", "uidt": "SingleLineText"}}
	fieldNames := nocoFieldNames(table)
	for _, field := range table.Fields {
		name := fieldNames[field.ID]
		columns = append(columns, map[string]any{"title": name, "column_name": name, "uidt": nocoType(field)})
	}
	var out struct {
		ID string `json:"id"`
	}
	payload := map[string]any{"title": table.Name, "table_name": table.Name, "columns": columns}
	if err := c.doJSON(ctx, http.MethodPost, "/api/v2/meta/bases/"+url.PathEscape(baseID)+"/tables", payload, &out); err != nil {
		return "", err
	}
	return out.ID, nil
}

func (c *Client) InsertRowsBatched(ctx context.Context, tableID string, rows []map[string]any, batchSize int) error {
	for start := 0; start < len(rows); start += batchSize {
		end := start + batchSize
		if end > len(rows) {
			end = len(rows)
		}
		payload := any(rows[start:end])
		if len(rows[start:end]) == 1 {
			payload = rows[start]
		}
		if err := c.doJSON(ctx, http.MethodPost, "/api/v2/tables/"+url.PathEscape(tableID)+"/records", payload, nil); err != nil {
			return err
		}
	}
	return nil
}

func nocoType(field airtable.Field) string {
	switch field.Type {
	case "number", "currency", "percent", "rating", "duration", "count":
		return "Number"
	case "checkbox":
		return "Checkbox"
	case "date", "dateTime", "createdTime", "lastModifiedTime":
		return "DateTime"
	case "multilineText", "richText", "multipleAttachments", "multipleRecordLinks", "multipleSelects":
		return "LongText"
	default:
		return "SingleLineText"
	}
}

func convertRows(table airtable.Table, rows []airtable.Record) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	fieldNames := nocoFieldNames(table)
	for _, row := range rows {
		next := map[string]any{"Airtable Record ID": row.ID}
		for _, field := range table.Fields {
			next[fieldNames[field.ID]] = value(row.Fields[field.ID])
		}
		out = append(out, next)
	}
	return out
}

func nocoFieldNames(table airtable.Table) map[string]string {
	used := map[string]int{nocoColumnKey("Airtable Record ID"): 1}
	out := map[string]string{}
	for _, field := range table.Fields {
		base := nocoFieldName(field.Name)
		name := base
		baseKey := nocoColumnKey(base)
		key := nocoColumnKey(name)
		if used[key] > 0 {
			for i := used[baseKey] + 1; ; i++ {
				name = fmt.Sprintf("%s %d", base, i)
				key = nocoColumnKey(name)
				if used[key] == 0 {
					used[baseKey] = i
					break
				}
			}
		}
		used[key]++
		if used[baseKey] == 0 {
			used[baseKey] = 1
		}
		out[field.ID] = name
	}
	return out
}

func nocoFieldName(name string) string {
	out := nocoName(name)
	switch nocoColumnKey(out) {
	case "", "id", "createdat", "updatedat", "created_at", "updated_at", "createdby", "updatedby", "created_by", "updated_by", "nc_order":
		if out == "" {
			return "Field"
		}
		return out + " Field"
	default:
		return out
	}
}

var nocoColumnKeyUnsafe = regexp.MustCompile(`[^a-z0-9]+`)

func nocoColumnKey(name string) string {
	key := strings.ToLower(strings.TrimSpace(name))
	key = nocoColumnKeyUnsafe.ReplaceAllString(key, "_")
	return strings.Trim(key, "_")
}

func value(v any) any {
	switch v.(type) {
	case nil, string, bool, float64, int, int64, json.Number:
		return v
	default:
		return common.Stringify(v)
	}
}

func (c *Client) doJSON(ctx context.Context, method, path string, payload any, out any) error {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("xc-auth", c.Token)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("nocodb API %s returned HTTP %d: %s", path, resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, out)
}
