package grist

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/nerveband/airvault/internal/airtable"
	"github.com/nerveband/airvault/internal/archive"
)

type Options struct {
	ArchivePath        string
	URL                string
	APIKey             string
	Cookie             string
	WorkspaceID        string
	DocID              string
	DryRun             bool
	IncludeAttachments bool
	IncludeFormulas    bool
	ReportPath         string
}

type Report struct {
	OK                  bool                 `json:"ok"`
	DryRun              bool                 `json:"dry_run"`
	OfflineCapable      bool                 `json:"offline_capable"`
	TargetURL           string               `json:"target_url,omitempty"`
	WorkspaceID         string               `json:"workspace_id,omitempty"`
	Bases               []BaseReport         `json:"bases"`
	FormulaTranslations []FormulaFieldReport `json:"formula_translations,omitempty"`
	Warnings            []string             `json:"warnings,omitempty"`
	Counts              map[string]int       `json:"counts"`
	Docs                map[string]string    `json:"docs,omitempty"`
}

type BaseReport struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	DocID  string `json:"doc_id,omitempty"`
	Tables int    `json:"tables"`
	Rows   int    `json:"rows"`
}

type FormulaFieldReport struct {
	BaseID string `json:"base_id"`
	Table  string `json:"table"`
	Field  string `json:"field"`
	FormulaTranslation
}

type Client struct {
	BaseURL    string
	APIKey     string
	Cookie     string
	HTTPClient *http.Client
}

func NewClient(baseURL, apiKey, cookie string) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), APIKey: apiKey, Cookie: cookie, HTTPClient: &http.Client{Timeout: 60 * time.Second}}
}

func Import(ctx context.Context, opts Options) (*Report, error) {
	if opts.ArchivePath == "" {
		return nil, fmt.Errorf("archive path is required")
	}
	if !opts.DryRun && opts.URL == "" {
		return nil, fmt.Errorf("grist url is required unless --dry-run is set")
	}
	if !opts.DryRun && opts.APIKey == "" && opts.Cookie == "" {
		return nil, fmt.Errorf("grist api key or cookie is required unless --dry-run is set")
	}
	if !opts.DryRun && opts.DocID == "" && opts.WorkspaceID == "" {
		return nil, fmt.Errorf("workspace id is required when creating Grist docs")
	}
	manifest, err := readManifest(opts.ArchivePath)
	if err != nil {
		return nil, err
	}
	report := &Report{
		OK:             true,
		DryRun:         opts.DryRun,
		OfflineCapable: true,
		TargetURL:      opts.URL,
		WorkspaceID:    opts.WorkspaceID,
		Counts:         map[string]int{"bases": 0, "tables": 0, "records": 0, "attachments": 0, "formulas": 0},
		Docs:           map[string]string{},
		Warnings: []string{
			"Grist is offline-capable when run as Grist Desktop or a self-hosted Docker service with local /persist storage",
			"Airtable views, interfaces, automations, permissions, and exact formula semantics are not one-to-one portable",
		},
	}
	client := NewClient(opts.URL, opts.APIKey, opts.Cookie)
	for _, base := range manifest.Bases {
		schema, err := readSchema(opts.ArchivePath, base.ID)
		if err != nil {
			return nil, err
		}
		docID := opts.DocID
		if docID == "" && opts.DryRun {
			docID = slug(base.Name)
		}
		if docID == "" {
			docID, err = client.CreateDoc(ctx, opts.WorkspaceID, base.Name)
			if err != nil {
				return nil, err
			}
		}
		report.Counts["bases"]++
		report.Docs[base.ID] = docID
		br := BaseReport{ID: base.ID, Name: base.Name, DocID: docID}
		for _, table := range schema.Tables {
			tableID := slug(table.Name)
			fieldIDsByName := fieldSlugMap(table)
			columns := buildColumns(base.ID, table, fieldIDsByName, opts.IncludeFormulas, report)
			if !opts.DryRun {
				if err := client.AddTable(ctx, docID, tableID, columns); err != nil {
					return nil, err
				}
			}
			rows, err := readRows(opts.ArchivePath, base.ID, table)
			if err != nil {
				return nil, err
			}
			attachmentCells := map[string]map[string][]int{}
			if opts.IncludeAttachments {
				attachmentCells, err = uploadAttachments(ctx, client, opts, docID, base.ID, table)
				if err != nil {
					return nil, err
				}
				for _, byField := range attachmentCells {
					for _, ids := range byField {
						report.Counts["attachments"] += len(ids)
					}
				}
			}
			records := make([]map[string]any, 0, len(rows))
			for _, row := range rows {
				records = append(records, convertRecord(row, table, fieldIDsByName, attachmentCells[row.ID]))
			}
			if len(records) > 0 && !opts.DryRun {
				if err := client.AddRecords(ctx, docID, tableID, records); err != nil {
					return nil, err
				}
			}
			br.Tables++
			br.Rows += len(records)
			report.Counts["tables"]++
			report.Counts["records"] += len(records)
		}
		report.Bases = append(report.Bases, br)
	}
	if opts.ReportPath != "" {
		if err := writeReport(opts.ReportPath, report); err != nil {
			return nil, err
		}
	}
	return report, nil
}

func buildColumns(baseID string, table airtable.Table, fieldIDsByName map[string]string, includeFormulas bool, report *Report) []map[string]any {
	columns := []map[string]any{{"id": "Airtable_Record_ID", "fields": map[string]any{"label": "Airtable Record ID", "type": "Text"}}}
	for _, field := range table.Fields {
		id := fieldIDsByName[field.Name]
		fields := map[string]any{"label": field.Name, "type": gristType(field)}
		if field.Type == "formula" {
			report.Counts["formulas"]++
			translation := TranslateFormula(formulaSource(field), fieldIDsByName)
			report.FormulaTranslations = append(report.FormulaTranslations, FormulaFieldReport{BaseID: baseID, Table: table.Name, Field: field.Name, FormulaTranslation: translation})
			if includeFormulas && translation.Output != "" {
				fields["formula"] = translation.Output
				fields["isFormula"] = true
			}
		}
		columns = append(columns, map[string]any{"id": id, "fields": fields})
	}
	return columns
}

func gristType(field airtable.Field) string {
	switch field.Type {
	case "number", "currency", "percent", "rating", "duration":
		return "Numeric"
	case "count":
		return "Int"
	case "checkbox":
		return "Bool"
	case "date":
		return "Date"
	case "dateTime", "createdTime", "lastModifiedTime":
		return "DateTime"
	case "singleSelect":
		return "Choice"
	case "multipleSelects":
		return "ChoiceList"
	case "multipleAttachments":
		return "Attachments"
	default:
		return "Text"
	}
}

func formulaSource(field airtable.Field) string {
	if field.Options == nil {
		return ""
	}
	if formula, ok := field.Options["formula"].(string); ok {
		return formula
	}
	return ""
}

func fieldSlugMap(table airtable.Table) map[string]string {
	used := map[string]int{}
	out := map[string]string{}
	for _, field := range table.Fields {
		base := slug(field.Name)
		id := base
		if used[base] > 0 {
			id = base + "_" + strconv.Itoa(used[base]+1)
		}
		used[base]++
		out[field.Name] = id
		out[field.ID] = id
	}
	return out
}

func convertRecord(rec airtable.Record, table airtable.Table, fieldIDsByName map[string]string, attachments map[string][]int) map[string]any {
	fields := map[string]any{"Airtable_Record_ID": rec.ID}
	for _, field := range table.Fields {
		id := fieldIDsByName[field.Name]
		if ids := attachments[field.Name]; len(ids) > 0 {
			fields[id] = append([]any{"L"}, intSliceToAny(ids)...)
			continue
		}
		if field.Type == "multipleAttachments" || field.Type == "formula" {
			continue
		}
		if field.Type == "multipleRecordLinks" {
			fields[id] = stringify(rec.Fields[field.ID])
			continue
		}
		fields[id] = rec.Fields[field.ID]
	}
	return fields
}

func intSliceToAny(ids []int) []any {
	out := make([]any, len(ids))
	for i, id := range ids {
		out[i] = id
	}
	return out
}

func uploadAttachments(ctx context.Context, client *Client, opts Options, docID, baseID string, table airtable.Table) (map[string]map[string][]int, error) {
	out := map[string]map[string][]int{}
	path := filepath.Join(opts.ArchivePath, "bases", baseID, "tables", table.ID, "attachments.jsonl")
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var ar archive.AttachmentRecord
		if err := json.Unmarshal(scanner.Bytes(), &ar); err != nil {
			return nil, err
		}
		if ar.Path == "" {
			continue
		}
		if opts.DryRun {
			if out[ar.RecordID] == nil {
				out[ar.RecordID] = map[string][]int{}
			}
			out[ar.RecordID][ar.FieldName] = append(out[ar.RecordID][ar.FieldName], 0)
			continue
		}
		id, err := client.UploadAttachment(ctx, docID, filepath.Join(opts.ArchivePath, ar.Path))
		if err != nil {
			return nil, err
		}
		if out[ar.RecordID] == nil {
			out[ar.RecordID] = map[string][]int{}
		}
		out[ar.RecordID][ar.FieldName] = append(out[ar.RecordID][ar.FieldName], id)
	}
	return out, scanner.Err()
}

func readManifest(root string) (*archive.Manifest, error) {
	var m archive.Manifest
	return &m, readJSON(filepath.Join(root, "manifest.json"), &m)
}

func readSchema(root, baseID string) (*airtable.Schema, error) {
	var s airtable.Schema
	return &s, readJSON(filepath.Join(root, "bases", baseID, "schema.json"), &s)
}

func readRows(root, baseID string, table airtable.Table) ([]airtable.Record, error) {
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

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

func writeReport(path string, report *Report) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0644)
}

func (c *Client) CreateDoc(ctx context.Context, workspaceID, name string) (string, error) {
	var out string
	if err := c.doJSON(ctx, http.MethodPost, "/api/workspaces/"+url.PathEscape(workspaceID)+"/docs", map[string]any{"name": name}, &out); err != nil {
		return "", err
	}
	return out, nil
}

func (c *Client) AddTable(ctx context.Context, docID, tableID string, columns []map[string]any) error {
	return c.doJSON(ctx, http.MethodPost, "/api/docs/"+url.PathEscape(docID)+"/tables", map[string]any{"tables": []map[string]any{{"id": tableID, "columns": columns}}}, nil)
}

func (c *Client) AddRecords(ctx context.Context, docID, tableID string, records []map[string]any) error {
	payload := map[string]any{"records": make([]map[string]any, 0, len(records))}
	for _, fields := range records {
		payload["records"] = append(payload["records"].([]map[string]any), map[string]any{"fields": fields})
	}
	return c.doJSON(ctx, http.MethodPost, "/api/docs/"+url.PathEscape(docID)+"/tables/"+url.PathEscape(tableID)+"/records", payload, nil)
}

func (c *Client) UploadAttachment(ctx context.Context, docID, path string) (int, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("upload", filepath.Base(path))
	if err != nil {
		return 0, err
	}
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	if _, err := io.Copy(part, file); err != nil {
		file.Close()
		return 0, err
	}
	file.Close()
	if err := writer.Close(); err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/docs/"+url.PathEscape(docID)+"/attachments", &body)
	if err != nil {
		return 0, err
	}
	c.auth(req)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return 0, fmt.Errorf("grist API %s returned HTTP %d: %s", req.URL.Path, resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var ids []int
	if err := json.Unmarshal(data, &ids); err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, fmt.Errorf("grist attachment upload returned no ids")
	}
	return ids[0], nil
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
	c.auth(req)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
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
		return fmt.Errorf("grist API %s returned HTTP %d: %s", path, resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, out)
}

func (c *Client) auth(req *http.Request) {
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	if c.Cookie != "" {
		req.Header.Set("Cookie", c.Cookie)
	}
}

var slugUnsafe = regexp.MustCompile(`[^A-Za-z0-9_]+`)

func slug(name string) string {
	out := slugUnsafe.ReplaceAllString(strings.TrimSpace(name), "_")
	out = strings.Trim(out, "_")
	if out == "" {
		return "Imported"
	}
	if out[0] >= '0' && out[0] <= '9' {
		out = "_" + out
	}
	return out
}

func stringify(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(data)
}
