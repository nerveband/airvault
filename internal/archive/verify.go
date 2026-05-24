package archive

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type VerifyMode string

const (
	VerifyManifest VerifyMode = "manifest"
	VerifyLedger   VerifyMode = "ledger"
	VerifyExists   VerifyMode = "exists"
	VerifySample   VerifyMode = "sample"
	VerifyFull     VerifyMode = "full"
	VerifyHealth   VerifyMode = "health"
)

type VerifyOptions struct {
	Mode       VerifyMode `json:"mode"`
	SampleSize int        `json:"sample_size,omitempty"`
}

type VerifyReport struct {
	OK       bool         `json:"ok"`
	Path     string       `json:"path"`
	Mode     VerifyMode   `json:"mode"`
	Duration string       `json:"duration"`
	Manifest *Manifest    `json:"manifest,omitempty"`
	Totals   Totals       `json:"totals"`
	Checks   VerifyChecks `json:"checks"`
	Problems []string     `json:"problems,omitempty"`
	Sampled  int          `json:"sampled,omitempty"`
}

type VerifyChecks struct {
	Manifest       bool `json:"manifest"`
	GapReport      bool `json:"gap_report"`
	Jobs           bool `json:"jobs"`
	Telemetry      bool `json:"telemetry"`
	Ledger         bool `json:"ledger"`
	AttachmentPath bool `json:"attachment_paths"`
	AttachmentSize bool `json:"attachment_sizes"`
	Checksums      bool `json:"checksums"`
}

func VerifyWithOptions(path string, opts VerifyOptions) (*VerifyReport, error) {
	start := time.Now()
	if opts.Mode == "" {
		opts.Mode = VerifyFull
	}
	report := &VerifyReport{OK: true, Path: path, Mode: opts.Mode}
	manifest, err := readManifest(path)
	if err != nil {
		return nil, err
	}
	report.Manifest = manifest
	report.Totals = manifest.Totals
	report.Checks.Manifest = true
	report.Checks.GapReport = fileExistsArchive(filepath.Join(path, "gap-report.json"))
	report.Checks.Jobs = dirExistsArchive(filepath.Join(path, "jobs"))
	report.Checks.Telemetry = fileExistsArchive(filepath.Join(path, "api-telemetry.json"))

	switch opts.Mode {
	case VerifyManifest:
	case VerifyLedger:
		verifyLedger(path, report)
	case VerifyExists:
		verifyLedger(path, report)
		verifyAttachmentFiles(path, report)
	case VerifySample, VerifyHealth:
		verifyLedger(path, report)
		verifyAttachmentFiles(path, report)
		verifyChecksums(path, report, opts.SampleSize)
	case VerifyFull:
		verifyLedger(path, report)
		verifyAttachmentFiles(path, report)
		verifyChecksums(path, report, 0)
	default:
		return nil, fmt.Errorf("unknown verify mode %q; supported: manifest,ledger,exists,sample,health,full", opts.Mode)
	}

	report.Duration = time.Since(start).String()
	if len(report.Problems) > 0 {
		report.OK = false
		return report, fmt.Errorf("%s", strings.Join(report.Problems, "; "))
	}
	return report, nil
}

func readManifest(path string) (*Manifest, error) {
	data, err := os.ReadFile(filepath.Join(path, "manifest.json"))
	if err != nil {
		return nil, err
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, err
	}
	return &manifest, nil
}

func verifyLedger(path string, report *VerifyReport) {
	var records, comments, attachments int
	var attachmentBytes int64
	err := filepath.WalkDir(filepath.Join(path, "bases"), func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		switch filepath.Base(p) {
		case "records.jsonl":
			n, err := countJSONLLines(p)
			if err != nil {
				return err
			}
			records += n
		case "comments.jsonl":
			n, err := countJSONLLines(p)
			if err != nil {
				return err
			}
			comments += n
		case "attachments.jsonl":
			file, err := os.Open(p)
			if err != nil {
				return err
			}
			defer file.Close()
			scanner := bufio.NewScanner(file)
			for scanner.Scan() {
				if strings.TrimSpace(scanner.Text()) == "" {
					continue
				}
				var ar AttachmentRecord
				if err := json.Unmarshal(scanner.Bytes(), &ar); err != nil {
					return err
				}
				attachments++
				attachmentBytes += ar.Attachment.Size
			}
			return scanner.Err()
		}
		return nil
	})
	if err != nil {
		report.Problems = append(report.Problems, "ledger scan failed: "+err.Error())
		return
	}
	report.Checks.Ledger = true
	if records != report.Totals.Records {
		report.Problems = append(report.Problems, fmt.Sprintf("record ledger mismatch: got %d want %d", records, report.Totals.Records))
	}
	if comments != report.Totals.Comments {
		report.Problems = append(report.Problems, fmt.Sprintf("comment ledger mismatch: got %d want %d", comments, report.Totals.Comments))
	}
	if attachments != report.Totals.Attachments {
		report.Problems = append(report.Problems, fmt.Sprintf("attachment ledger mismatch: got %d want %d", attachments, report.Totals.Attachments))
	}
	if attachmentBytes != report.Totals.AttachmentBytes {
		report.Problems = append(report.Problems, fmt.Sprintf("attachment byte ledger mismatch: got %d want %d", attachmentBytes, report.Totals.AttachmentBytes))
	}
}

func verifyAttachmentFiles(path string, report *VerifyReport) {
	missing := 0
	sizeMismatch := 0
	err := eachAttachmentRecord(path, func(ar AttachmentRecord) error {
		if ar.Path == "" {
			return nil
		}
		info, err := os.Stat(filepath.Join(path, ar.Path))
		if err != nil {
			missing++
			return nil
		}
		if info.Size() != ar.Attachment.Size {
			sizeMismatch++
		}
		return nil
	})
	if err != nil {
		report.Problems = append(report.Problems, "attachment existence scan failed: "+err.Error())
		return
	}
	report.Checks.AttachmentPath = missing == 0
	report.Checks.AttachmentSize = sizeMismatch == 0
	if missing > 0 {
		report.Problems = append(report.Problems, fmt.Sprintf("missing attachment files: %d", missing))
	}
	if sizeMismatch > 0 {
		report.Problems = append(report.Problems, fmt.Sprintf("attachment size mismatches: %d", sizeMismatch))
	}
}

func verifyChecksums(path string, report *VerifyReport, sampleSize int) {
	checkPath := filepath.Join(path, "checksums.sha256")
	file, err := os.Open(checkPath)
	if err != nil {
		report.Problems = append(report.Problems, "checksums unavailable: "+err.Error())
		return
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	checked := 0
	for scanner.Scan() {
		if sampleSize > 0 && checked >= sampleSize {
			break
		}
		parts := strings.Fields(scanner.Text())
		if len(parts) < 2 {
			continue
		}
		got, err := fileSHA256(filepath.Join(path, parts[1]))
		if err != nil {
			report.Problems = append(report.Problems, fmt.Sprintf("checksum read failed for %s: %v", parts[1], err))
			continue
		}
		if got != parts[0] {
			report.Problems = append(report.Problems, "checksum mismatch for "+parts[1])
		}
		checked++
	}
	if err := scanner.Err(); err != nil {
		report.Problems = append(report.Problems, "checksum scan failed: "+err.Error())
	}
	report.Sampled = checked
	report.Checks.Checksums = len(report.Problems) == 0
}

func countJSONLLines(path string) (int, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	count := 0
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) != "" {
			count++
		}
	}
	return count, scanner.Err()
}

func eachAttachmentRecord(path string, fn func(AttachmentRecord) error) error {
	return filepath.WalkDir(filepath.Join(path, "bases"), func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Base(p) != "attachments.jsonl" {
			return err
		}
		file, err := os.Open(p)
		if err != nil {
			return err
		}
		defer file.Close()
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			if strings.TrimSpace(scanner.Text()) == "" {
				continue
			}
			var ar AttachmentRecord
			if err := json.Unmarshal(scanner.Bytes(), &ar); err != nil {
				return err
			}
			if err := fn(ar); err != nil {
				return err
			}
		}
		return scanner.Err()
	})
}

func fileExistsArchive(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExistsArchive(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
