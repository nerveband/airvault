package archive

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/nerveband/airvault/internal/airtable"
)

type Manifest struct {
	ArchiveVersion string        `json:"archive_version"`
	ToolVersion    string        `json:"tool_version"`
	StartedAt      time.Time     `json:"started_at"`
	FinishedAt     time.Time     `json:"finished_at,omitempty"`
	Bases          []BaseSummary `json:"bases"`
	Totals         Totals        `json:"totals"`
	Unsupported    []string      `json:"unsupported"`
}

type BaseSummary struct {
	ID              string         `json:"id"`
	Name            string         `json:"name"`
	PermissionLevel string         `json:"permission_level,omitempty"`
	Tables          []TableSummary `json:"tables"`
	Records         int            `json:"records"`
	Attachments     int            `json:"attachments"`
	AttachmentBytes int64          `json:"attachment_bytes"`
}

type TableSummary struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Records         int    `json:"records"`
	Attachments     int    `json:"attachments"`
	AttachmentBytes int64  `json:"attachment_bytes"`
}

type Totals struct {
	Bases           int   `json:"bases"`
	Tables          int   `json:"tables"`
	Records         int   `json:"records"`
	Attachments     int   `json:"attachments"`
	AttachmentBytes int64 `json:"attachment_bytes"`
}

type Options struct {
	Out                 string
	BaseIDs             []string
	DownloadAttachments bool
	ToolVersion         string
	DryRun              bool
}

type AttachmentRecord struct {
	BaseID       string              `json:"base_id"`
	TableID      string              `json:"table_id"`
	TableName    string              `json:"table_name"`
	RecordID     string              `json:"record_id"`
	FieldName    string              `json:"field_name"`
	Attachment   airtable.Attachment `json:"attachment"`
	Path         string              `json:"path,omitempty"`
	SHA256       string              `json:"sha256,omitempty"`
	DownloadedAt time.Time           `json:"downloaded_at,omitempty"`
	Error        string              `json:"error,omitempty"`
}

func Estimate(ctx context.Context, client *airtable.Client, bases []airtable.Base) (*Manifest, error) {
	return run(ctx, client, bases, Options{DryRun: true, DownloadAttachments: false, ToolVersion: "estimate"})
}

func Create(ctx context.Context, client *airtable.Client, bases []airtable.Base, opts Options) (*Manifest, error) {
	return run(ctx, client, bases, opts)
}

func Verify(path string) (*Manifest, error) {
	data, err := os.ReadFile(filepath.Join(path, "manifest.json"))
	if err != nil {
		return nil, err
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, err
	}
	checkPath := filepath.Join(path, "checksums.sha256")
	f, err := os.Open(checkPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		parts := strings.Fields(scanner.Text())
		if len(parts) < 2 {
			continue
		}
		want, rel := parts[0], parts[1]
		got, err := fileSHA256(filepath.Join(path, rel))
		if err != nil {
			return nil, fmt.Errorf("verify %s: %w", rel, err)
		}
		if got != want {
			return nil, fmt.Errorf("checksum mismatch for %s", rel)
		}
	}
	return &manifest, scanner.Err()
}

func run(ctx context.Context, client *airtable.Client, bases []airtable.Base, opts Options) (*Manifest, error) {
	manifest := &Manifest{
		ArchiveVersion: "airvault-archive-v1",
		ToolVersion:    opts.ToolVersion,
		StartedAt:      time.Now(),
		Unsupported: []string{
			"interfaces: Airtable public APIs do not expose full interface definitions for portable restore",
			"automations: Airtable public APIs do not expose full automation definitions for portable restore",
			"permissions: collaborator and workspace permission state is not preserved",
			"extensions: extension configuration is not preserved",
		},
	}
	baseFilter := map[string]bool{}
	for _, id := range opts.BaseIDs {
		baseFilter[id] = true
	}
	var checksum *os.File
	if !opts.DryRun {
		if err := os.MkdirAll(opts.Out, 0755); err != nil {
			return nil, err
		}
		var err error
		checksum, err = os.Create(filepath.Join(opts.Out, "checksums.sha256"))
		if err != nil {
			return nil, err
		}
		defer checksum.Close()
	}
	for _, base := range bases {
		if len(baseFilter) > 0 && !baseFilter[base.ID] {
			continue
		}
		schema, err := client.Schema(ctx, base.ID)
		if err != nil {
			return nil, err
		}
		bsum := BaseSummary{ID: base.ID, Name: base.Name, PermissionLevel: base.PermissionLevel}
		manifest.Totals.Bases++
		manifest.Totals.Tables += len(schema.Tables)
		baseDir := filepath.Join(opts.Out, "bases", base.ID)
		if !opts.DryRun {
			if err := writeJSON(filepath.Join(baseDir, "base.json"), base); err != nil {
				return nil, err
			}
			if err := writeJSON(filepath.Join(baseDir, "schema.json"), schema); err != nil {
				return nil, err
			}
		}
		for _, table := range schema.Tables {
			tsum := TableSummary{ID: table.ID, Name: table.Name}
			recordsPath := filepath.Join(baseDir, "tables", table.ID, "records.jsonl")
			attachmentsPath := filepath.Join(baseDir, "tables", table.ID, "attachments.jsonl")
			var recordsFile, attachmentsFile *os.File
			if !opts.DryRun {
				if err := os.MkdirAll(filepath.Dir(recordsPath), 0755); err != nil {
					return nil, err
				}
				var err error
				recordsFile, err = os.Create(recordsPath)
				if err != nil {
					return nil, err
				}
				defer recordsFile.Close()
				attachmentsFile, err = os.Create(attachmentsPath)
				if err != nil {
					return nil, err
				}
				defer attachmentsFile.Close()
				if err := writeJSON(filepath.Join(baseDir, "tables", table.ID, "table.json"), table); err != nil {
					return nil, err
				}
			}
			attachmentFields := map[string]string{}
			for _, field := range table.Fields {
				if field.Type == "multipleAttachments" {
					attachmentFields[field.ID] = field.Name
				}
			}
			err := client.EachRecord(ctx, base.ID, table.Name, func(rec airtable.Record) error {
				tsum.Records++
				if !opts.DryRun {
					if err := writeJSONLine(recordsFile, rec); err != nil {
						return err
					}
				}
				for fieldID, fieldName := range attachmentFields {
					for _, att := range airtable.AttachmentsFromValue(rec.Fields[fieldID]) {
						tsum.Attachments++
						tsum.AttachmentBytes += att.Size
						ar := AttachmentRecord{BaseID: base.ID, TableID: table.ID, TableName: table.Name, RecordID: rec.ID, FieldName: fieldName, Attachment: att}
						if !opts.DryRun && opts.DownloadAttachments {
							rel := filepath.Join("bases", base.ID, "attachments", table.ID, rec.ID, sanitize(att.ID+"__"+att.Filename))
							ar.Path = rel
							full := filepath.Join(opts.Out, rel)
							if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
								return err
							}
							f, err := os.Create(full)
							if err != nil {
								return err
							}
							h := sha256.New()
							err = client.Download(ctx, att.URL, io.MultiWriter(f, h))
							closeErr := f.Close()
							if err != nil {
								ar.Error = err.Error()
							} else if closeErr != nil {
								ar.Error = closeErr.Error()
							} else {
								ar.SHA256 = hex.EncodeToString(h.Sum(nil))
								ar.DownloadedAt = time.Now()
								fmt.Fprintf(checksum, "%s  %s\n", ar.SHA256, rel)
							}
						}
						if !opts.DryRun {
							if err := writeJSONLine(attachmentsFile, ar); err != nil {
								return err
							}
						}
					}
				}
				return nil
			})
			if err != nil {
				return nil, err
			}
			bsum.Records += tsum.Records
			bsum.Attachments += tsum.Attachments
			bsum.AttachmentBytes += tsum.AttachmentBytes
			bsum.Tables = append(bsum.Tables, tsum)
		}
		manifest.Totals.Records += bsum.Records
		manifest.Totals.Attachments += bsum.Attachments
		manifest.Totals.AttachmentBytes += bsum.AttachmentBytes
		manifest.Bases = append(manifest.Bases, bsum)
	}
	manifest.FinishedAt = time.Now()
	if !opts.DryRun {
		if err := writeJSON(filepath.Join(opts.Out, "manifest.json"), manifest); err != nil {
			return nil, err
		}
		if err := writeJSON(filepath.Join(opts.Out, "gap-report.json"), manifest.Unsupported); err != nil {
			return nil, err
		}
	}
	return manifest, nil
}

func writeJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func writeJSONLine(w io.Writer, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(data))
	return err
}

var unsafePath = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func sanitize(name string) string {
	name = unsafePath.ReplaceAllString(name, "_")
	name = strings.Trim(name, "._")
	if name == "" {
		return "attachment"
	}
	return name
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
