package archive

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nerveband/airvault/internal/airtable"
)

func WriteFixture(path string) (*Manifest, error) {
	baseID := "appFixture"
	tableID := "tblFixture"
	jobID := "job_fixture"
	manifest := &Manifest{
		ArchiveVersion: "airvault-archive-v1",
		ToolVersion:    "fixture",
		JobID:          jobID,
		Selection:      Selection{Components: DefaultComponents(), BaseIDs: []string{baseID}, TableIDsOrNames: []string{tableID}},
		StartedAt:      time.Now(),
		FinishedAt:     time.Now(),
		Bases: []BaseSummary{{
			ID: baseID, Name: "Fixture Base",
			Tables:  []TableSummary{{ID: tableID, Name: "Fixture Table", Records: 2, Attachments: 1, AttachmentBytes: 12}},
			Records: 2, Attachments: 1, AttachmentBytes: 12,
		}},
		Totals: Totals{Bases: 1, Tables: 1, Records: 2, Attachments: 1, AttachmentBytes: 12},
		APITelemetry: &airtable.Telemetry{
			StartedAt: time.Now(), FinishedAt: time.Now(), StatusCounts: map[int]int64{}, ByBase: map[string]airtable.BaseStats{},
		},
		Unsupported: []string{"fixture: unsupported surfaces are intentionally represented"},
	}
	baseDir := filepath.Join(path, "bases", baseID)
	tableDir := filepath.Join(baseDir, "tables", tableID)
	attDir := filepath.Join(baseDir, "attachments", tableID, "recFixture1")
	if err := os.MkdirAll(tableDir, 0755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(attDir, 0755); err != nil {
		return nil, err
	}
	if err := writeJSON(filepath.Join(baseDir, "base.json"), airtable.Base{ID: baseID, Name: "Fixture Base"}); err != nil {
		return nil, err
	}
	schema := &airtable.Schema{Tables: []airtable.Table{{ID: tableID, Name: "Fixture Table", Fields: []airtable.Field{
		{ID: "fldName", Name: "Name", Type: "singleLineText"},
		{ID: "fldScore", Name: "Score", Type: "number"},
		{ID: "fldDouble", Name: "Double Score", Type: "formula", Options: map[string]any{"formula": "{Score} * 2"}},
		{ID: "fldFile", Name: "File", Type: "multipleAttachments"},
	}}}}
	if err := writeJSON(filepath.Join(baseDir, "schema.json"), schema); err != nil {
		return nil, err
	}
	if err := writeJSON(filepath.Join(tableDir, "table.json"), schema.Tables[0]); err != nil {
		return nil, err
	}
	recordsPath := filepath.Join(tableDir, "records.jsonl")
	records, err := os.Create(recordsPath)
	if err != nil {
		return nil, err
	}
	for i := 1; i <= 2; i++ {
		if err := writeJSONLine(records, airtable.Record{ID: fmt.Sprintf("recFixture%d", i), Fields: map[string]any{"fldName": fmt.Sprintf("Fixture %d", i), "fldScore": i}}); err != nil {
			records.Close()
			return nil, err
		}
	}
	records.Close()
	attPath := filepath.Join(attDir, "attFixture__fixture.txt")
	if err := os.WriteFile(attPath, []byte("fixture-data"), 0644); err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte("fixture-data"))
	hash := hex.EncodeToString(sum[:])
	rel, _ := filepath.Rel(path, attPath)
	if err := os.WriteFile(filepath.Join(path, "checksums.sha256"), []byte(hash+"  "+rel+"\n"), 0644); err != nil {
		return nil, err
	}
	attachments, err := os.Create(filepath.Join(tableDir, "attachments.jsonl"))
	if err != nil {
		return nil, err
	}
	if err := writeJSONLine(attachments, AttachmentRecord{BaseID: baseID, TableID: tableID, TableName: "Fixture Table", RecordID: "recFixture1", FieldName: "File", Attachment: airtable.Attachment{ID: "attFixture", Filename: "fixture.txt", Size: 12}, Path: rel, SHA256: hash}); err != nil {
		attachments.Close()
		return nil, err
	}
	attachments.Close()
	if err := os.WriteFile(filepath.Join(tableDir, "comments.jsonl"), nil, 0644); err != nil {
		return nil, err
	}
	if err := writeJSON(filepath.Join(path, "gap-report.json"), manifest.Unsupported); err != nil {
		return nil, err
	}
	if err := writeJSON(filepath.Join(path, "api-telemetry.json"), manifest.APITelemetry); err != nil {
		return nil, err
	}
	if err := writeJSON(filepath.Join(path, "manifest.json"), manifest); err != nil {
		return nil, err
	}
	if err := writeJSON(filepath.Join(path, "jobs", jobID+".json"), manifest); err != nil {
		return nil, err
	}
	return manifest, nil
}
