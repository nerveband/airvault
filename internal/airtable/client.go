package airtable

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const API = "https://api.airtable.com/v0"

type Client struct {
	Token      string
	HTTPClient *http.Client
}

func New(token string) *Client {
	return &Client{Token: token, HTTPClient: &http.Client{Timeout: 60 * time.Second}}
}

type Base struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	PermissionLevel string `json:"permissionLevel,omitempty"`
}

type BasesResponse struct {
	Bases []Base `json:"bases"`
}

type Field struct {
	ID      string         `json:"id"`
	Name    string         `json:"name"`
	Type    string         `json:"type"`
	Options map[string]any `json:"options,omitempty"`
}

type View struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type Table struct {
	ID             string  `json:"id"`
	Name           string  `json:"name"`
	PrimaryFieldID string  `json:"primaryFieldId,omitempty"`
	Fields         []Field `json:"fields"`
	Views          []View  `json:"views,omitempty"`
}

type Schema struct {
	Tables []Table `json:"tables"`
}

type Record struct {
	ID          string         `json:"id"`
	CreatedTime string         `json:"createdTime,omitempty"`
	Fields      map[string]any `json:"fields"`
}

type RecordsPage struct {
	Records []Record `json:"records"`
	Offset  string   `json:"offset,omitempty"`
}

type Attachment struct {
	ID         string         `json:"id"`
	URL        string         `json:"url"`
	Filename   string         `json:"filename"`
	Size       int64          `json:"size"`
	Type       string         `json:"type,omitempty"`
	Thumbnails map[string]any `json:"thumbnails,omitempty"`
}

func (c *Client) ListBases(ctx context.Context) ([]Base, error) {
	var out BasesResponse
	if err := c.getJSON(ctx, API+"/meta/bases", &out); err != nil {
		return nil, err
	}
	return out.Bases, nil
}

func (c *Client) Schema(ctx context.Context, baseID string) (*Schema, error) {
	var out Schema
	if err := c.getJSON(ctx, API+"/meta/bases/"+url.PathEscape(baseID)+"/tables", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) EachRecord(ctx context.Context, baseID, tableName string, fn func(Record) error) error {
	offset := ""
	for {
		values := url.Values{"pageSize": {"100"}, "cellFormat": {"json"}, "returnFieldsByFieldId": {"true"}}
		if offset != "" {
			values.Set("offset", offset)
		}
		var page RecordsPage
		endpoint := API + "/" + url.PathEscape(baseID) + "/" + url.PathEscape(tableName) + "?" + values.Encode()
		if err := c.getJSON(ctx, endpoint, &page); err != nil {
			return err
		}
		for _, rec := range page.Records {
			if err := fn(rec); err != nil {
				return err
			}
		}
		if page.Offset == "" {
			return nil
		}
		offset = page.Offset
		time.Sleep(220 * time.Millisecond)
	}
}

func (c *Client) Download(ctx context.Context, rawurl string, w io.Writer) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawurl, nil)
	if err != nil {
		return err
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("download returned HTTP %d", resp.StatusCode)
	}
	_, err = io.Copy(w, resp.Body)
	return err
}

func (c *Client) getJSON(ctx context.Context, endpoint string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/json")
	for attempt := 0; attempt < 5; attempt++ {
		resp, err := c.HTTPClient.Do(req.Clone(ctx))
		if err != nil {
			return err
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return readErr
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			sleep := time.Duration(attempt+1) * time.Second
			if retryAfter := resp.Header.Get("Retry-After"); retryAfter != "" {
				if seconds, err := strconv.Atoi(retryAfter); err == nil {
					sleep = time.Duration(seconds) * time.Second
				}
			}
			time.Sleep(sleep)
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			return fmt.Errorf("airtable HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}
		return json.Unmarshal(body, out)
	}
	return fmt.Errorf("airtable rate limit did not clear after retries")
}

func AttachmentsFromValue(v any) []Attachment {
	items, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]Attachment, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		att := Attachment{}
		if s, ok := m["id"].(string); ok {
			att.ID = s
		}
		if s, ok := m["url"].(string); ok {
			att.URL = s
		}
		if s, ok := m["filename"].(string); ok {
			att.Filename = s
		}
		if s, ok := m["type"].(string); ok {
			att.Type = s
		}
		if n, ok := m["size"].(float64); ok {
			att.Size = int64(n)
		}
		if att.URL != "" {
			out = append(out, att)
		}
	}
	return out
}
