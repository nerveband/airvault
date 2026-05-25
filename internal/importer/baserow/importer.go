package baserow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/mail"
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
			"Supported Airtable fields are explicitly mapped to Baserow field types; unsupported values are preserved as JSON text",
			"Airtable views, interfaces, automations, comments, permissions, and exact formula semantics are not one-to-one portable",
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
				tableID, err := client.CreateTable(ctx, appID, table.Name)
				if err != nil {
					return nil, err
				}
				names := baserowFieldNames(table)
				for _, field := range table.Fields {
					if err := client.CreateField(ctx, tableID, field, names[field.ID]); err != nil {
						return nil, err
					}
				}
				if err := client.InsertRowsBatched(ctx, tableID, convertRows(table, rows, names), 200); err != nil {
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
	if err := c.doJSON(ctx, http.MethodPost, fmt.Sprintf("/api/applications/workspace/%d/", workspaceID), map[string]any{"name": name, "type": "database"}, &out); err != nil {
		return 0, err
	}
	return out.ID, nil
}

func (c *Client) CreateTable(ctx context.Context, databaseID int, name string) (int, error) {
	var out struct {
		ID int `json:"id"`
	}
	payload := map[string]any{"name": name, "data": [][]any{{"Airtable Record ID"}}, "first_row_header": true}
	if err := c.doJSON(ctx, http.MethodPost, fmt.Sprintf("/api/database/tables/database/%d/", databaseID), payload, &out); err != nil {
		return 0, err
	}
	return out.ID, nil
}

func (c *Client) CreateField(ctx context.Context, tableID int, field airtable.Field, name string) error {
	return c.doJSON(ctx, http.MethodPost, fmt.Sprintf("/api/database/fields/table/%d/", tableID), baserowFieldPayload(field, name), nil)
}

func (c *Client) InsertRowsBatched(ctx context.Context, tableID int, rows []map[string]any, batchSize int) error {
	if batchSize <= 0 {
		batchSize = 200
	}
	for start := 0; start < len(rows); start += batchSize {
		end := start + batchSize
		if end > len(rows) {
			end = len(rows)
		}
		payload := map[string]any{"items": rows[start:end]}
		if err := c.doJSON(ctx, http.MethodPost, fmt.Sprintf("/api/database/rows/table/%d/batch/?user_field_names=true", tableID), payload, nil); err != nil {
			return err
		}
	}
	return nil
}

func baserowFieldPayload(field airtable.Field, name string) map[string]any {
	payload := map[string]any{"name": name, "type": baserowType(field)}
	switch payload["type"] {
	case "number":
		payload["number_decimal_places"] = 10
		payload["number_negative"] = true
	case "rating":
		payload["max_value"] = ratingMax(field)
		payload["color"] = "yellow"
		payload["style"] = "star"
	case "date":
		payload["date_format"] = "ISO"
		payload["date_include_time"] = field.Type == "dateTime" || field.Type == "createdTime" || field.Type == "lastModifiedTime"
		payload["date_time_format"] = "24"
	case "duration":
		payload["duration_format"] = "h:mm:ss"
	case "single_select", "multiple_select":
		payload["select_options"] = selectOptions(field)
	case "long_text":
		if field.Type == "richText" {
			payload["long_text_enable_rich_text"] = true
		}
	}
	return payload
}

func baserowType(field airtable.Field) string {
	switch field.Type {
	case "multilineText", "richText", "multipleRecordLinks", "multipleCollaborators", "createdBy", "lastModifiedBy", "externalSyncSource":
		return "long_text"
	case "url":
		return "url"
	case "email":
		return "email"
	case "phoneNumber":
		return "phone_number"
	case "number", "currency", "percent", "count", "autoNumber":
		return "number"
	case "rating":
		return "rating"
	case "checkbox":
		return "boolean"
	case "date", "dateTime", "createdTime", "lastModifiedTime":
		return "date"
	case "duration":
		return "duration"
	case "singleSelect":
		return "single_select"
	case "multipleSelects":
		return "multiple_select"
	case "multipleAttachments":
		return "file"
	default:
		return "text"
	}
}

func convertRows(table airtable.Table, rows []airtable.Record, names map[string]string) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		next := map[string]any{"Airtable Record ID": row.ID}
		for _, field := range table.Fields {
			if value := baserowValue(field, row.Fields[field.ID]); value != nil {
				next[names[field.ID]] = value
			}
		}
		out = append(out, next)
	}
	return out
}

func baserowValue(field airtable.Field, v any) any {
	switch field.Type {
	case "number", "currency", "percent", "duration":
		if n, ok := numberValue(v); ok {
			return math.Round(n*1e10) / 1e10
		}
		return nil
	case "url":
		if s, ok := v.(string); ok {
			return cleanURL(s)
		}
		return nil
	case "email":
		if s, ok := v.(string); ok && validEmail(s) {
			return s
		}
		return nil
	case "phoneNumber":
		if s, ok := v.(string); ok && validPhone(s) {
			return s
		}
		return nil
	case "multipleSelects":
		return stringSlice(v)
	case "singleSelect":
		if s, ok := v.(string); ok {
			s = strings.TrimSpace(s)
			if s == "" {
				return nil
			}
			return s
		}
		return nil
	case "multipleAttachments":
		return []any{}
	case "multipleRecordLinks", "multipleCollaborators", "createdBy", "lastModifiedBy", "externalSyncSource":
		return common.Stringify(v)
	default:
		switch v.(type) {
		case nil, string, bool, float64, int, int64, json.Number:
			return v
		default:
			return common.Stringify(v)
		}
	}
}

func numberValue(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

var phoneAllowed = regexp.MustCompile(`^[0-9+().\-\s]{3,}$`)

func validPhone(s string) bool {
	return phoneAllowed.MatchString(strings.TrimSpace(s))
}

func cleanURL(s string) any {
	cleaned := strings.ReplaceAll(strings.TrimSpace(s), " ", "%20")
	parsed, err := url.Parse(cleaned)
	if err != nil {
		return nil
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil
	}
	out := parsed.String()
	if strings.ContainsAny(out, " \t\r\n") {
		return nil
	}
	return out
}

func validEmail(s string) bool {
	_, err := mail.ParseAddress(strings.TrimSpace(s))
	return err == nil
}

func stringSlice(v any) any {
	items, ok := v.([]any)
	if !ok {
		return v
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			out = append(out, s)
		}
	}
	return out
}

func baserowFieldNames(table airtable.Table) map[string]string {
	used := map[string]int{"airtable record id": 1}
	out := map[string]string{}
	for _, field := range table.Fields {
		name := strings.TrimSpace(field.Name)
		if name == "" {
			name = "Field"
		}
		base := name
		key := strings.ToLower(name)
		if used[key] > 0 {
			for i := used[strings.ToLower(base)] + 1; ; i++ {
				name = fmt.Sprintf("%s %d", base, i)
				key = strings.ToLower(name)
				if used[key] == 0 {
					break
				}
			}
		}
		used[key]++
		out[field.ID] = name
	}
	return out
}

func ratingMax(field airtable.Field) int {
	if field.Options != nil {
		if max, ok := field.Options["max"].(float64); ok && max >= 1 && max <= 10 {
			return int(max)
		}
	}
	return 5
}

func selectOptions(field airtable.Field) []map[string]any {
	out := []map[string]any{}
	if field.Options == nil {
		return out
	}
	choices, ok := field.Options["choices"].([]any)
	if !ok {
		return out
	}
	for _, raw := range choices {
		choice, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name, _ := choice["name"].(string)
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		out = append(out, map[string]any{"value": name, "color": "blue"})
	}
	return out
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
	var data []byte
	var status int
	for attempt := 0; attempt < 6; attempt++ {
		resp, err := c.HTTPClient.Do(req.Clone(ctx))
		if err != nil {
			return err
		}
		data, _ = io.ReadAll(resp.Body)
		resp.Body.Close()
		status = resp.StatusCode
		if status != http.StatusConflict && status != http.StatusTooManyRequests {
			break
		}
		time.Sleep(time.Duration(attempt+1) * 750 * time.Millisecond)
	}
	if status < 200 || status > 299 {
		return fmt.Errorf("baserow API %s returned HTTP %d: %s", path, status, strings.TrimSpace(string(data)))
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, out)
}
