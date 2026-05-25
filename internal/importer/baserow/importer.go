package baserow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/nerveband/airvault/internal/airtable"
	"github.com/nerveband/airvault/internal/importer/common"
)

type Options struct {
	ArchivePath string
	URL         string
	Token       string
	WorkspaceID int
	DryRun      bool
}

type Report struct {
	OK             bool           `json:"ok"`
	DryRun         bool           `json:"dry_run"`
	OfflineCapable bool           `json:"offline_capable"`
	TargetURL      string         `json:"target_url,omitempty"`
	WorkspaceID    int            `json:"workspace_id,omitempty"`
	Counts         map[string]int `json:"counts"`
	Bases          []BaseReport   `json:"bases"`
	Warnings       []string       `json:"warnings,omitempty"`
}

type BaseReport struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	TargetID int    `json:"target_id,omitempty"`
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
		return nil, fmt.Errorf("baserow url is required unless --dry-run is set")
	}
	if !opts.DryRun && opts.Token == "" {
		return nil, fmt.Errorf("baserow token is required unless --dry-run is set")
	}
	if !opts.DryRun && opts.WorkspaceID == 0 {
		return nil, fmt.Errorf("baserow workspace id is required unless --dry-run is set")
	}
	manifest, err := common.ReadManifest(opts.ArchivePath)
	if err != nil {
		return nil, err
	}
	report := &Report{
		OK: true, DryRun: opts.DryRun, OfflineCapable: true, TargetURL: opts.URL, WorkspaceID: opts.WorkspaceID,
		Counts: map[string]int{"bases": 0, "tables": 0, "records": 0},
		Warnings: []string{
			"Baserow is offline-capable when self-hosted with local Docker volumes",
			"Airtable views, formulas, interfaces, automations, comments, and exact attachment fields are not one-to-one portable",
			"Tables are created through Baserow's data import endpoint so rows are visible immediately for comparison",
		},
	}
	client := NewClient(opts.URL, opts.Token)
	for _, base := range manifest.Bases {
		schema, err := common.ReadSchema(opts.ArchivePath, base.ID)
		if err != nil {
			return nil, err
		}
		appID := 0
		if !opts.DryRun {
			appID, err = client.CreateDatabase(ctx, opts.WorkspaceID, "Airvault - "+base.Name)
			if err != nil {
				return nil, err
			}
		}
		br := BaseReport{ID: base.ID, Name: base.Name, TargetID: appID}
		report.Counts["bases"]++
		for _, table := range schema.Tables {
			rows, err := common.ReadRows(opts.ArchivePath, base.ID, table)
			if err != nil {
				return nil, err
			}
			if !opts.DryRun {
				if _, err := client.CreateTable(ctx, appID, table, rows); err != nil {
					return nil, err
				}
			}
			br.Tables++
			br.Rows += len(rows)
			report.Counts["tables"]++
			report.Counts["records"] += len(rows)
		}
		report.Bases = append(report.Bases, br)
	}
	return report, nil
}

func NewClient(baseURL, token string) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), Token: token, HTTPClient: &http.Client{Timeout: 120 * time.Second}}
}

func (c *Client) CreateDatabase(ctx context.Context, workspaceID int, name string) (int, error) {
	var out struct {
		ID int `json:"id"`
	}
	payload := map[string]any{"name": name, "type": "database"}
	if err := c.doJSON(ctx, http.MethodPost, fmt.Sprintf("/api/applications/workspace/%d/", workspaceID), payload, &out); err != nil {
		return 0, err
	}
	return out.ID, nil
}

func (c *Client) CreateTable(ctx context.Context, databaseID int, table airtable.Table, rows []airtable.Record) (int, error) {
	data := tableData(table, rows)
	var out struct {
		ID int `json:"id"`
	}
	payload := map[string]any{"name": table.Name, "data": data, "first_row_header": true}
	if err := c.doJSON(ctx, http.MethodPost, fmt.Sprintf("/api/database/tables/database/%d/", databaseID), payload, &out); err != nil {
		return 0, err
	}
	return out.ID, nil
}

func tableData(table airtable.Table, rows []airtable.Record) [][]any {
	header := []any{"Airtable Record ID"}
	for _, field := range table.Fields {
		header = append(header, field.Name)
	}
	out := [][]any{header}
	for _, row := range rows {
		next := []any{row.ID}
		for _, field := range table.Fields {
			next = append(next, value(row.Fields[field.ID]))
		}
		out = append(out, next)
	}
	return out
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
	req.Header.Set("Authorization", "JWT "+c.Token)
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
		return fmt.Errorf("baserow API %s returned HTTP %d: %s", path, resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, out)
}

func EscapePath(s string) string {
	return url.PathEscape(s)
}
