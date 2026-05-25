package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nerveband/airvault/internal/airtable"
	"github.com/nerveband/airvault/internal/archive"
	"github.com/nerveband/airvault/internal/config"
	"github.com/nerveband/airvault/internal/exporter"
	baserowimport "github.com/nerveband/airvault/internal/importer/baserow"
	gristimport "github.com/nerveband/airvault/internal/importer/grist"
	nocodbimport "github.com/nerveband/airvault/internal/importer/nocodb"
	"github.com/nerveband/airvault/internal/output"
	"github.com/nerveband/airvault/internal/update"
	"github.com/spf13/cobra"
)

var (
	Version    = "dev"
	format     = "auto"
	token      = ""
	profile    = ""
	jsonOutput bool
)

func Execute() {
	var updateCh <-chan update.CheckResult
	if update.ShouldCheckUpdates(os.Args[1:]) {
		updateCh = update.CheckAsync(Version)
	}
	cmd := root()
	if err := cmd.Execute(); err != nil {
		os.Exit(output.HandleError(err, format))
	}
	if updateCh != nil {
		if notice := update.FormatNotice(<-updateCh); notice != "" {
			fmt.Fprint(os.Stderr, notice)
		}
	}
}

func root() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "airvault",
		Short:         "Comprehensive Airtable backup CLI",
		Long:          "Airvault backs up Airtable schemas, records, linked record IDs, attachments, and gap reports into a verifiable local archive.",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if jsonOutput {
				format = "json"
			}
		},
	}
	cmd.PersistentFlags().StringVar(&format, "format", "auto", "Output format: auto, json, ndjson, table")
	cmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "Alias for --format json")
	cmd.PersistentFlags().StringVar(&token, "token", "", "Airtable PAT (prefer AIRTABLE_TOKEN)")
	cmd.PersistentFlags().StringVar(&profile, "profile", "", "Named profile from ~/.airvault/config.json")
	cmd.AddCommand(versionCmd(), schemaCmd(), agentContextCmd(), skillPathCmd(), authCmd(), basesCmd(), estimateCmd(), backupCmd(), verifyCmd(), exportCmd(), importCmd(), testCmd(), profileCmd(), configCmd(), jobsCmd(), feedbackCmd(), upgradeCmd())
	return cmd
}

func client() (*airtable.Client, error) {
	t := token
	if t == "" {
		t = os.Getenv("AIRTABLE_TOKEN")
	}
	if t == "" && profile != "" {
		store, err := config.Load()
		if err == nil {
			if p, ok := store.Profiles[profile]; ok {
				if p.TokenEnv != "" {
					t = os.Getenv(p.TokenEnv)
				}
				if t == "" {
					t = p.Token
				}
			}
		}
	}
	if t == "" {
		return nil, &output.Error{Code: "CONFIG_MISSING_TOKEN", Message: "Airtable token is required", Hint: "Set AIRTABLE_TOKEN or pass --token", ExitCode: output.ExitConfig}
	}
	return airtable.New(t), nil
}

func versionCmd() *cobra.Command {
	return &cobra.Command{Use: "version", Short: "Print version", Example: "  airvault version --format json", RunE: func(cmd *cobra.Command, args []string) error {
		return output.Write(cmd.OutOrStdout(), format, map[string]string{"version": Version}, func(w io.Writer) error {
			_, err := fmt.Fprintln(w, Version)
			return err
		})
	}}
}

func authCmd() *cobra.Command {
	parent := &cobra.Command{Use: "auth", Short: "Authentication helpers"}
	parent.AddCommand(&cobra.Command{Use: "doctor", Short: "Validate Airtable token", Example: "  AIRTABLE_TOKEN=pat... airvault auth doctor --format json", RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client()
		if err != nil {
			return err
		}
		bases, err := c.ListBases(context.Background())
		if err != nil {
			return apiErr(err)
		}
		return output.Write(cmd.OutOrStdout(), format, map[string]any{"ok": true, "visible_bases": len(bases)}, nil)
	}})
	return parent
}

func basesCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "bases", Short: "Work with Airtable bases"}
	var idOnly bool
	var countOnly bool
	var limit int
	cmd.AddCommand(&cobra.Command{Use: "list", Short: "List visible bases", Example: "  airvault bases list --format json\n  airvault bases list --id-only", RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client()
		if err != nil {
			return err
		}
		bases, err := c.ListBases(context.Background())
		if err != nil {
			return apiErr(err)
		}
		if limit > 0 && limit < len(bases) {
			bases = bases[:limit]
		}
		if countOnly {
			return output.Write(cmd.OutOrStdout(), format, map[string]any{"count": len(bases)}, nil)
		}
		if idOnly {
			ids := make([]string, 0, len(bases))
			for _, b := range bases {
				ids = append(ids, b.ID)
			}
			return output.Write(cmd.OutOrStdout(), format, ids, nil)
		}
		return output.Write(cmd.OutOrStdout(), format, bases, func(w io.Writer) error {
			for _, b := range bases {
				fmt.Fprintf(w, "%s\t%s\t%s\n", b.ID, b.Name, b.PermissionLevel)
			}
			return nil
		})
	}})
	list := cmd.Commands()[0]
	list.Flags().BoolVar(&idOnly, "id-only", false, "Return only base IDs")
	list.Flags().BoolVar(&countOnly, "count", false, "Return only count")
	list.Flags().IntVar(&limit, "limit", 0, "Limit number of bases returned")
	return cmd
}

func estimateCmd() *cobra.Command {
	var baseIDs []string
	var tableIDs []string
	cmd := &cobra.Command{Use: "estimate", Short: "Estimate records and attachment storage without downloading files", Example: "  airvault estimate --format json\n  airvault estimate --base appXXXXXXXX --table Students", RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client()
		if err != nil {
			return err
		}
		bases, err := c.ListBases(context.Background())
		if err != nil {
			return apiErr(err)
		}
		filtered := filterBases(bases, baseIDs)
		m, err := archive.Create(context.Background(), c, filtered, archive.Options{DryRun: true, DownloadAttachments: false, ToolVersion: "estimate", Selection: archive.Selection{BaseIDs: baseIDs, TableIDsOrNames: tableIDs, Components: archive.DefaultComponents()}})
		if err != nil {
			return apiErr(err)
		}
		return output.Write(cmd.OutOrStdout(), format, m, nil)
	}}
	cmd.Flags().StringSliceVar(&baseIDs, "base", nil, "Base ID to include; repeat or comma-separate")
	cmd.Flags().StringSliceVar(&tableIDs, "table", nil, "Table ID or name to include; repeat or comma-separate")
	return cmd
}

func backupCmd() *cobra.Command {
	var out string
	var baseIDs []string
	var tableIDs []string
	var include []string
	var exclude []string
	var dryRun bool
	var noAttachments bool
	var maxAttachmentBytes int64
	var resumeJob string
	cmd := &cobra.Command{Use: "backup", Short: "Create and verify Airtable backups"}
	create := &cobra.Command{Use: "create", Short: "Create a local backup archive", Example: "  airvault backup create --out ./airtable-backup --format json\n  airvault backup create --base appXXXXXXXX --include schema,records --out ./schema-records\n  airvault backup create --table Students --exclude attachments --out ./students", RunE: func(cmd *cobra.Command, args []string) error {
		defaults := loadDefaults()
		if out == "" && defaults.BackupRoot != "" {
			out = filepath.Join(defaults.BackupRoot, "runs", time.Now().Format("2006-01-02_150405"))
		}
		if out == "" {
			return &output.Error{Code: "VALIDATION_MISSING_OUT", Message: "--out is required", Hint: "Run: airvault backup create --out ./airtable-backup or configure backup_root", ExitCode: output.ExitValidation}
		}
		if len(include) == 0 {
			include = defaults.Include
		}
		if len(exclude) == 0 {
			exclude = defaults.Exclude
		}
		components, err := archive.ParseComponents(include, exclude)
		if err != nil {
			return &output.Error{Code: "VALIDATION_BAD_COMPONENT", Message: err.Error(), ExitCode: output.ExitValidation}
		}
		if noAttachments || defaults.NoAttachments {
			components.Attachments = false
		}
		if maxAttachmentBytes == 0 {
			maxAttachmentBytes = defaults.MaxAttachmentBytes
		}
		c, err := client()
		if err != nil {
			return err
		}
		bases, err := c.ListBases(context.Background())
		if err != nil {
			return apiErr(err)
		}
		m, err := archive.Create(context.Background(), c, filterBases(bases, baseIDs), archive.Options{
			Out: out, BaseIDs: baseIDs, DownloadAttachments: components.Attachments, ToolVersion: Version, DryRun: dryRun, ResumeJobID: resumeJob,
			Selection: archive.Selection{BaseIDs: baseIDs, TableIDsOrNames: tableIDs, Components: components, MaxAttachmentBytes: maxAttachmentBytes},
		})
		if err != nil {
			return apiErr(err)
		}
		return output.Write(cmd.OutOrStdout(), format, m, nil)
	}}
	create.Flags().StringVar(&out, "out", "", "Backup output directory")
	create.Flags().StringSliceVar(&baseIDs, "base", nil, "Base ID to include; repeat or comma-separate")
	create.Flags().StringSliceVar(&tableIDs, "table", nil, "Table ID or name to include; repeat or comma-separate")
	create.Flags().StringSliceVar(&include, "include", nil, "Components to include: schema,records,attachments,comments,views,all")
	create.Flags().StringSliceVar(&exclude, "exclude", nil, "Components to exclude: schema,records,attachments,comments,views")
	create.Flags().BoolVar(&dryRun, "dry-run", false, "Plan backup without writing files")
	create.Flags().BoolVar(&noAttachments, "no-attachments", false, "Record attachment metadata but do not download files")
	create.Flags().Int64Var(&maxAttachmentBytes, "max-attachment-bytes", 0, "Skip attachment files larger than this many bytes")
	create.Flags().StringVar(&resumeJob, "resume-job", "", "Resume using an existing job ID")
	cmd.AddCommand(create)
	cmd.AddCommand(verifyCommand())
	return cmd
}

func verifyCmd() *cobra.Command {
	return verifyCommand()
}

func verifyCommand() *cobra.Command {
	var mode string
	var sampleSize int
	cmd := &cobra.Command{Use: "verify --path <backup>", Short: "Verify backup integrity", Example: "  airvault backup verify --path ./backup --mode exists\n  airvault backup verify --path ./backup --mode sample --sample-size 50\n  airvault backup verify --path ./backup --mode full", RunE: func(cmd *cobra.Command, args []string) error {
		defaults := loadDefaults()
		path, _ := cmd.Flags().GetString("path")
		if path == "" {
			return &output.Error{Code: "VALIDATION_MISSING_PATH", Message: "--path is required", ExitCode: output.ExitValidation}
		}
		if !cmd.Flags().Changed("mode") && defaults.VerifyMode != "" {
			mode = defaults.VerifyMode
		}
		if !cmd.Flags().Changed("sample-size") && defaults.SampleSize > 0 {
			sampleSize = defaults.SampleSize
		}
		report, err := archive.VerifyWithOptions(path, archive.VerifyOptions{Mode: archive.VerifyMode(mode), SampleSize: sampleSize})
		if err != nil {
			return &output.Error{Code: "VERIFY_FAILED", Message: err.Error(), ExitCode: output.ExitConflict}
		}
		return output.Write(cmd.OutOrStdout(), format, report, nil)
	}}
	cmd.Flags().String("path", "", "Backup archive path")
	cmd.Flags().StringVar(&mode, "mode", "full", "Verify mode: manifest, ledger, exists, sample, health, full")
	cmd.Flags().IntVar(&sampleSize, "sample-size", 25, "Files to hash in sample/health mode")
	return cmd
}

func schemaCmd() *cobra.Command {
	var validate bool
	cmd := &cobra.Command{Use: "schema", Short: "Print machine-readable command schema", Example: "  airvault schema --format json\n  airvault schema --validate", RunE: func(cmd *cobra.Command, args []string) error {
		if validate {
			return output.Write(cmd.OutOrStdout(), format, map[string]any{"ok": true, "schema_version": "airvault-cli-schema-v2"}, nil)
		}
		return output.Write(cmd.OutOrStdout(), "json", commandSchema(), nil)
	}}
	cmd.Flags().BoolVar(&validate, "validate", false, "Validate CLI contract")
	return cmd
}

func agentContextCmd() *cobra.Command {
	return &cobra.Command{Use: "agent-context", Short: "Print agent-oriented usage contract", RunE: func(cmd *cobra.Command, args []string) error {
		return output.Write(cmd.OutOrStdout(), "json", map[string]any{
			"name":           "airvault",
			"version":        Version,
			"schema_version": "airvault-cli-schema-v2",
			"defaults":       loadDefaults(),
			"guardrails": []string{
				"Use AIRTABLE_TOKEN instead of --token when possible",
				"Run estimate before large live backups",
				"Use backup verify --mode exists for fast routine checks",
				"Use backup verify --mode full only when every attachment hash must be rechecked",
				"Rotate Airtable PATs after emergency backup sessions",
				"Review api-telemetry.json and gap-report.json after backups",
			},
			"common_workflows": []string{
				"airvault config defaults --format json",
				"airvault estimate --format json",
				"airvault backup create --format json",
				"airvault backup verify --path ./airtable-backup --mode exists --format json",
				"airvault test export --path ./airtable-backup --out /tmp/airvault-export-test --format json",
			},
		}, nil)
	}}
}

func skillPathCmd() *cobra.Command {
	return &cobra.Command{Use: "skill-path", Short: "Print path to bundled SKILL.md", Example: "  airvault skill-path", RunE: func(cmd *cobra.Command, args []string) error {
		wd, _ := os.Getwd()
		return output.Write(cmd.OutOrStdout(), format, map[string]string{"path": wd + "/SKILL.md"}, func(w io.Writer) error {
			_, err := fmt.Fprintln(w, wd+"/SKILL.md")
			return err
		})
	}}
}

func exportCmd() *cobra.Command {
	var path, out, deliver string
	var overwrite, plan bool
	cmd := &cobra.Command{Use: "export <jsonl|sqlite|postgres>", Short: "Export an airvault archive to another format", Args: cobra.ExactArgs(1), Example: "  airvault export sqlite --path ./airtable-backup --out airtable.sqlite --overwrite\n  airvault export postgres --path ./airtable-backup --out airtable.sql\n  airvault export jsonl --path ./airtable-backup --out ./jsonl", RunE: func(cmd *cobra.Command, args []string) error {
		if path == "" {
			return &output.Error{Code: "VALIDATION_MISSING_PATH", Message: "--path is required", ExitCode: output.ExitValidation}
		}
		e, err := exporter.Get(args[0])
		if err != nil {
			return &output.Error{Code: "VALIDATION_BAD_EXPORTER", Message: err.Error(), ExitCode: output.ExitValidation}
		}
		opts := exporter.Options{ArchivePath: path, Out: out, Deliver: deliver, Overwrite: overwrite}
		var result *exporter.Result
		if plan {
			result, err = e.Plan(context.Background(), opts)
		} else {
			result, err = e.Export(context.Background(), opts)
		}
		if err != nil {
			return &output.Error{Code: "EXPORT_FAILED", Message: err.Error(), ExitCode: output.ExitConflict}
		}
		if deliver != "" {
			return output.Deliver(deliver, overwrite, func(w io.Writer) error {
				return json.NewEncoder(w).Encode(result)
			})
		}
		return output.Write(cmd.OutOrStdout(), format, result, nil)
	}}
	cmd.Flags().StringVar(&path, "path", "", "Archive path")
	cmd.Flags().StringVar(&out, "out", "", "Output path or directory")
	cmd.Flags().StringVar(&deliver, "deliver", "", "Deliver result metadata to stdout or file:<path>")
	cmd.Flags().BoolVar(&overwrite, "overwrite", false, "Overwrite existing output")
	cmd.Flags().BoolVar(&plan, "plan", false, "Plan export without writing")
	return cmd
}

func importCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "import", Short: "Import an airvault archive into another system"}
	var path, url, apiKey, cookie, workspaceID, docID, report string
	var dryRun, includeAttachments, includeFormulas bool
	grist := &cobra.Command{Use: "grist", Short: "Import an archive into a local or self-hosted Grist server", Example: "  airvault import grist --path ./airtable-backup --dry-run --format json\n  airvault import grist --path ./airtable-backup --url http://localhost:8484 --workspace 3 --api-key $GRIST_API_KEY --include-formulas --include-attachments", RunE: func(cmd *cobra.Command, args []string) error {
		if path == "" {
			return &output.Error{Code: "VALIDATION_MISSING_PATH", Message: "--path is required", ExitCode: output.ExitValidation}
		}
		if apiKey == "" {
			apiKey = os.Getenv("GRIST_API_KEY")
		}
		if cookie == "" {
			cookie = os.Getenv("GRIST_COOKIE")
		}
		result, err := gristimport.Import(context.Background(), gristimport.Options{
			ArchivePath: path, URL: url, APIKey: apiKey, Cookie: cookie, WorkspaceID: workspaceID, DocID: docID, DryRun: dryRun,
			IncludeAttachments: includeAttachments, IncludeFormulas: includeFormulas, ReportPath: report,
		})
		if err != nil {
			return &output.Error{Code: "IMPORT_FAILED", Message: err.Error(), ExitCode: output.ExitConflict}
		}
		return output.Write(cmd.OutOrStdout(), format, result, nil)
	}}
	grist.Flags().StringVar(&path, "path", "", "Archive path")
	grist.Flags().StringVar(&url, "url", "", "Grist base URL, for example http://localhost:8484")
	grist.Flags().StringVar(&apiKey, "api-key", "", "Grist API key; defaults to GRIST_API_KEY")
	grist.Flags().StringVar(&cookie, "cookie", "", "Grist session cookie for local bootstrapped tests; defaults to GRIST_COOKIE")
	grist.Flags().StringVar(&workspaceID, "workspace", "", "Grist workspace ID used when creating docs")
	grist.Flags().StringVar(&docID, "doc", "", "Existing Grist doc ID; if set, imports all bases into this doc")
	grist.Flags().StringVar(&report, "report", "", "Write migration report JSON to this path")
	grist.Flags().BoolVar(&dryRun, "dry-run", false, "Plan import without contacting or writing to Grist")
	grist.Flags().BoolVar(&includeAttachments, "include-attachments", false, "Upload local attachment files into Grist attachment columns")
	grist.Flags().BoolVar(&includeFormulas, "include-formulas", false, "Create Grist formula columns from best-effort Airtable formula translations")
	cmd.AddCommand(grist)

	var nocoURL, nocoToken string
	var nocoBatchSize int
	nocodb := &cobra.Command{Use: "nocodb", Short: "Import an archive into a local or self-hosted NocoDB server", Example: "  airvault import nocodb --path ./airtable-backup --dry-run --format json\n  airvault import nocodb --path ./airtable-backup --url http://localhost:8080 --token $NOCODB_TOKEN --format json", RunE: func(cmd *cobra.Command, args []string) error {
		if path == "" {
			return &output.Error{Code: "VALIDATION_MISSING_PATH", Message: "--path is required", ExitCode: output.ExitValidation}
		}
		if nocoToken == "" {
			nocoToken = os.Getenv("NOCODB_TOKEN")
		}
		result, err := nocodbimport.Import(context.Background(), nocodbimport.Options{ArchivePath: path, URL: nocoURL, Token: nocoToken, DryRun: dryRun, BatchSize: nocoBatchSize})
		if err != nil {
			return &output.Error{Code: "IMPORT_FAILED", Message: err.Error(), ExitCode: output.ExitConflict}
		}
		return output.Write(cmd.OutOrStdout(), format, result, nil)
	}}
	nocodb.Flags().StringVar(&path, "path", "", "Archive path")
	nocodb.Flags().StringVar(&nocoURL, "url", "", "NocoDB base URL, for example http://localhost:8080")
	nocodb.Flags().StringVar(&nocoToken, "token", "", "NocoDB auth token; defaults to NOCODB_TOKEN")
	nocodb.Flags().BoolVar(&dryRun, "dry-run", false, "Plan import without contacting or writing to NocoDB")
	nocodb.Flags().IntVar(&nocoBatchSize, "batch-size", 100, "Rows per NocoDB insert batch")
	cmd.AddCommand(nocodb)

	var baserowURL, baserowToken string
	var baserowWorkspace int
	baserow := &cobra.Command{Use: "baserow", Short: "Import an archive into a local or self-hosted Baserow server", Example: "  airvault import baserow --path ./airtable-backup --dry-run --format json\n  airvault import baserow --path ./airtable-backup --url http://localhost:8081 --token $BASEROW_TOKEN --workspace 1 --format json", RunE: func(cmd *cobra.Command, args []string) error {
		if path == "" {
			return &output.Error{Code: "VALIDATION_MISSING_PATH", Message: "--path is required", ExitCode: output.ExitValidation}
		}
		if baserowToken == "" {
			baserowToken = os.Getenv("BASEROW_TOKEN")
		}
		result, err := baserowimport.Import(context.Background(), baserowimport.Options{ArchivePath: path, URL: baserowURL, Token: baserowToken, WorkspaceID: baserowWorkspace, DryRun: dryRun})
		if err != nil {
			return &output.Error{Code: "IMPORT_FAILED", Message: err.Error(), ExitCode: output.ExitConflict}
		}
		return output.Write(cmd.OutOrStdout(), format, result, nil)
	}}
	baserow.Flags().StringVar(&path, "path", "", "Archive path")
	baserow.Flags().StringVar(&baserowURL, "url", "", "Baserow base URL, for example http://localhost:8081")
	baserow.Flags().StringVar(&baserowToken, "token", "", "Baserow JWT token; defaults to BASEROW_TOKEN")
	baserow.Flags().IntVar(&baserowWorkspace, "workspace", 0, "Baserow workspace ID")
	baserow.Flags().BoolVar(&dryRun, "dry-run", false, "Plan import without contacting or writing to Baserow")
	cmd.AddCommand(baserow)
	return cmd
}

func loadDefaults() config.Defaults {
	store, err := config.Load()
	if err != nil {
		return config.BuiltinDefaults()
	}
	return store.Defaults
}

func testCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "test", Short: "Run backup integrity tests"}
	var out string
	var overwrite bool
	fixture := &cobra.Command{Use: "fixture", Short: "Create a local fixture archive", Example: "  airvault test fixture --out /tmp/airvault-fixture --overwrite", RunE: func(cmd *cobra.Command, args []string) error {
		if out == "" {
			return &output.Error{Code: "VALIDATION_MISSING_OUT", Message: "--out is required", ExitCode: output.ExitValidation}
		}
		if overwrite {
			if err := os.RemoveAll(out); err != nil {
				return err
			}
		}
		if _, err := os.Stat(out); err == nil {
			return &output.Error{Code: "VALIDATION_OUTPUT_EXISTS", Message: out + " exists; pass --overwrite", ExitCode: output.ExitConflict}
		}
		m, err := archive.WriteFixture(out)
		if err != nil {
			return err
		}
		return output.Write(cmd.OutOrStdout(), format, map[string]any{"ok": true, "manifest": m}, nil)
	}}
	fixture.Flags().StringVar(&out, "out", "", "Fixture output directory")
	fixture.Flags().BoolVar(&overwrite, "overwrite", false, "Replace existing fixture directory")
	cmd.AddCommand(fixture)

	var path string
	var verifyMode string
	var verifySampleSize int
	verify := &cobra.Command{Use: "verify", Short: "Verify an archive and report integrity checks", Example: "  airvault test verify --path ./airtable-backup --mode exists --format json", RunE: func(cmd *cobra.Command, args []string) error {
		if path == "" {
			return &output.Error{Code: "VALIDATION_MISSING_PATH", Message: "--path is required", ExitCode: output.ExitValidation}
		}
		report, err := archive.VerifyWithOptions(path, archive.VerifyOptions{Mode: archive.VerifyMode(verifyMode), SampleSize: verifySampleSize})
		if err != nil {
			return &output.Error{Code: "TEST_VERIFY_FAILED", Message: err.Error(), ExitCode: output.ExitConflict}
		}
		return output.Write(cmd.OutOrStdout(), format, report, nil)
	}}
	verify.Flags().StringVar(&path, "path", "", "Archive path")
	verify.Flags().StringVar(&verifyMode, "mode", "exists", "Verify mode: manifest, ledger, exists, sample, health, full")
	verify.Flags().IntVar(&verifySampleSize, "sample-size", 25, "Files to hash in sample/health mode")
	cmd.AddCommand(verify)

	var exportOut string
	var exporters []string
	exportTest := &cobra.Command{Use: "export", Short: "Test archive exports", Example: "  airvault test export --path ./airtable-backup --out /tmp/airvault-export-test --format json", RunE: func(cmd *cobra.Command, args []string) error {
		if path == "" || exportOut == "" {
			return &output.Error{Code: "VALIDATION_MISSING_INPUT", Message: "--path and --out are required", ExitCode: output.ExitValidation}
		}
		if len(exporters) == 0 {
			exporters = loadDefaults().Exporters
		}
		results := []any{}
		for _, name := range exporters {
			e, err := exporter.Get(name)
			if err != nil {
				return &output.Error{Code: "VALIDATION_BAD_EXPORTER", Message: err.Error(), ExitCode: output.ExitValidation}
			}
			target := exportTarget(exportOut, name)
			result, err := e.Export(context.Background(), exporter.Options{ArchivePath: path, Out: target, Overwrite: true})
			if err != nil {
				return &output.Error{Code: "TEST_EXPORT_FAILED", Message: err.Error(), ExitCode: output.ExitConflict}
			}
			results = append(results, result)
		}
		return output.Write(cmd.OutOrStdout(), format, map[string]any{"ok": true, "exports": results}, nil)
	}}
	exportTest.Flags().StringVar(&path, "path", "", "Archive path")
	exportTest.Flags().StringVar(&exportOut, "out", "", "Export test output directory")
	exportTest.Flags().StringSliceVar(&exporters, "exporter", nil, "Exporter to test; repeat or comma-separate")
	cmd.AddCommand(exportTest)

	full := &cobra.Command{Use: "full", Short: "Run full fixture-only integrity test", Example: "  airvault test full --out /tmp/airvault-test --overwrite --format json", RunE: func(cmd *cobra.Command, args []string) error {
		if out == "" {
			return &output.Error{Code: "VALIDATION_MISSING_OUT", Message: "--out is required", ExitCode: output.ExitValidation}
		}
		if overwrite {
			if err := os.RemoveAll(out); err != nil {
				return err
			}
		}
		fixtureDir := out + "/fixture"
		exportDir := out + "/exports"
		if _, err := archive.WriteFixture(fixtureDir); err != nil {
			return err
		}
		m, err := archive.Verify(fixtureDir)
		if err != nil {
			return err
		}
		exportResults := []any{}
		for _, name := range loadDefaults().Exporters {
			e, err := exporter.Get(name)
			if err != nil {
				return err
			}
			result, err := e.Export(context.Background(), exporter.Options{ArchivePath: fixtureDir, Out: exportTarget(exportDir, name), Overwrite: true})
			if err != nil {
				return err
			}
			exportResults = append(exportResults, result)
		}
		return output.Write(cmd.OutOrStdout(), format, map[string]any{
			"ok":           true,
			"archive_path": fixtureDir,
			"checks":       map[string]bool{"fixture": true, "verify": true, "exports": true},
			"totals":       m.Totals,
			"exports":      exportResults,
		}, nil)
	}}
	full.Flags().StringVar(&out, "out", "", "Test output directory")
	full.Flags().BoolVar(&overwrite, "overwrite", false, "Replace existing test output")
	cmd.AddCommand(full)
	return cmd
}

func exportTarget(root, name string) string {
	switch name {
	case "sqlite":
		return root + "/airvault.sqlite"
	case "postgres":
		return root + "/airvault.sql"
	case "jsonl":
		return root + "/jsonl"
	default:
		return root + "/" + name
	}
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func profileCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "profile", Short: "Manage named profiles"}
	cmd.AddCommand(&cobra.Command{Use: "list", Short: "List profiles", Example: "  airvault profile list --format json", RunE: func(cmd *cobra.Command, args []string) error {
		store, err := config.Load()
		if err != nil {
			return err
		}
		return output.Write(cmd.OutOrStdout(), format, store.Redacted(), nil)
	}})
	var name, tokenEnv, rawToken string
	set := &cobra.Command{Use: "set", Short: "Set a profile", Example: "  airvault profile set --name personal --token-env AIRTABLE_TOKEN", RunE: func(cmd *cobra.Command, args []string) error {
		if name == "" {
			return &output.Error{Code: "VALIDATION_MISSING_NAME", Message: "--name is required", ExitCode: output.ExitValidation}
		}
		store, err := config.Load()
		if err != nil {
			return err
		}
		store.Profiles[name] = config.Profile{Name: name, TokenEnv: tokenEnv, Token: rawToken}
		if store.Default == "" {
			store.Default = name
		}
		if err := config.Save(store); err != nil {
			return err
		}
		return output.Write(cmd.OutOrStdout(), format, store.Redacted().Profiles[name], nil)
	}}
	set.Flags().StringVar(&name, "name", "", "Profile name")
	set.Flags().StringVar(&tokenEnv, "token-env", "", "Environment variable containing Airtable token")
	set.Flags().StringVar(&rawToken, "token-value", "", "Token value to store locally; prefer --token-env")
	cmd.AddCommand(set)
	return cmd
}

func configCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "config", Short: "Inspect configuration"}
	cmd.AddCommand(&cobra.Command{Use: "inspect", Short: "Inspect redacted config", Example: "  airvault config inspect --format json", RunE: func(cmd *cobra.Command, args []string) error {
		store, err := config.Load()
		if err != nil {
			return err
		}
		return output.Write(cmd.OutOrStdout(), format, map[string]any{"precedence": []string{"explicit flags", "--token", "AIRTABLE_TOKEN", "--profile token env", "--profile token", "config defaults", "builtin defaults"}, "config": store.Redacted()}, nil)
	}})
	cmd.AddCommand(&cobra.Command{Use: "defaults", Short: "Show effective defaults", Example: "  airvault config defaults --format json", RunE: func(cmd *cobra.Command, args []string) error {
		return output.Write(cmd.OutOrStdout(), format, loadDefaults(), nil)
	}})
	var backupRoot, verifyMode string
	var include, exclude, exporters []string
	var sampleSize int
	var noAttachments bool
	var maxAttachmentBytes int64
	setDefaults := &cobra.Command{Use: "set-defaults", Short: "Set workflow defaults", Example: "  airvault config set-defaults --backup-root ./airtable-backups --verify-mode exists --sample-size 25", RunE: func(cmd *cobra.Command, args []string) error {
		store, err := config.Load()
		if err != nil {
			return err
		}
		d := store.Defaults
		if cmd.Flags().Changed("backup-root") {
			d.BackupRoot = backupRoot
		}
		if cmd.Flags().Changed("include") {
			d.Include = include
		}
		if cmd.Flags().Changed("exclude") {
			d.Exclude = exclude
		}
		if cmd.Flags().Changed("verify-mode") {
			d.VerifyMode = verifyMode
		}
		if cmd.Flags().Changed("sample-size") {
			d.SampleSize = sampleSize
		}
		if cmd.Flags().Changed("exporter") {
			d.Exporters = exporters
		}
		if cmd.Flags().Changed("no-attachments") {
			d.NoAttachments = noAttachments
		}
		if cmd.Flags().Changed("max-attachment-bytes") {
			d.MaxAttachmentBytes = maxAttachmentBytes
		}
		store.Defaults = config.MergeDefaults(config.BuiltinDefaults(), d)
		if err := config.Save(store); err != nil {
			return err
		}
		return output.Write(cmd.OutOrStdout(), format, store.Redacted().Defaults, nil)
	}}
	setDefaults.Flags().StringVar(&backupRoot, "backup-root", "", "Default backup root; backup create without --out writes runs/<timestamp>")
	setDefaults.Flags().StringSliceVar(&include, "include", nil, "Default backup components")
	setDefaults.Flags().StringSliceVar(&exclude, "exclude", nil, "Default excluded backup components")
	setDefaults.Flags().StringVar(&verifyMode, "verify-mode", "exists", "Default verify mode")
	setDefaults.Flags().IntVar(&sampleSize, "sample-size", 25, "Default sample size")
	setDefaults.Flags().StringSliceVar(&exporters, "exporter", nil, "Default exporters")
	setDefaults.Flags().BoolVar(&noAttachments, "no-attachments", false, "Default to metadata-only attachments")
	setDefaults.Flags().Int64Var(&maxAttachmentBytes, "max-attachment-bytes", 0, "Default max attachment size")
	cmd.AddCommand(setDefaults)
	return cmd
}

func jobsCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "jobs", Short: "Inspect backup job ledgers"}
	var path string
	list := &cobra.Command{Use: "list", Short: "List jobs in archive", Example: "  airvault jobs list --path ./airtable-backup --format json", RunE: func(cmd *cobra.Command, args []string) error {
		if path == "" {
			return &output.Error{Code: "VALIDATION_MISSING_PATH", Message: "--path is required", ExitCode: output.ExitValidation}
		}
		entries, err := os.ReadDir(path + "/jobs")
		if err != nil {
			return err
		}
		names := []string{}
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
				names = append(names, strings.TrimSuffix(entry.Name(), ".json"))
			}
		}
		return output.Write(cmd.OutOrStdout(), format, names, nil)
	}}
	list.Flags().StringVar(&path, "path", "", "Archive path")
	cmd.AddCommand(list)
	return cmd
}

func feedbackCmd() *cobra.Command {
	return &cobra.Command{Use: "feedback <message>", Short: "Record local CLI feedback", Args: cobra.ExactArgs(1), Example: "  airvault feedback \"backup help should mention --include\"", RunE: func(cmd *cobra.Command, args []string) error {
		home, _ := os.UserHomeDir()
		path := home + "/.airvault/feedback.jsonl"
		if err := os.MkdirAll(home+"/.airvault", 0700); err != nil {
			return err
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		defer f.Close()
		row := map[string]any{"message": args[0], "version": Version}
		if err := json.NewEncoder(f).Encode(row); err != nil {
			return err
		}
		return output.Write(cmd.OutOrStdout(), format, map[string]any{"ok": true, "path": path}, nil)
	}}
}

func upgradeCmd() *cobra.Command {
	return &cobra.Command{Use: "upgrade", Short: "Upgrade airvault from GitHub Releases", RunE: func(cmd *cobra.Command, args []string) error {
		_, err := update.Upgrade(Version)
		return err
	}}
}

func commandSchema() map[string]any {
	return map[string]any{
		"schema_version": "airvault-cli-schema-v2",
		"commands": []map[string]any{
			{"name": "auth doctor", "readonly": true, "idempotent": true},
			{"name": "bases list", "readonly": true, "idempotent": true},
			{"name": "estimate", "readonly": true, "idempotent": true, "flags": []string{"--base", "--table"}},
			{"name": "backup create", "readonly": false, "destructive": false, "idempotent": true, "dry_run": true, "scope": "remote-read/local-write", "flags": []string{"--out", "--base", "--table", "--include", "--exclude", "--dry-run", "--no-attachments", "--max-attachment-bytes", "--resume-job"}},
			{"name": "backup verify", "readonly": true, "idempotent": true, "flags": []string{"--path", "--mode", "--sample-size"}},
			{"name": "export", "readonly": false, "destructive": false, "idempotent": true, "flags": []string{"--path", "--out", "--deliver", "--overwrite", "--plan"}},
			{"name": "import grist", "readonly": false, "destructive": false, "idempotent": false, "dry_run": true, "scope": "local-read/grist-write", "flags": []string{"--path", "--url", "--api-key", "--cookie", "--workspace", "--doc", "--dry-run", "--include-attachments", "--include-formulas", "--report"}},
			{"name": "import nocodb", "readonly": false, "destructive": false, "idempotent": false, "dry_run": true, "scope": "local-read/nocodb-write", "flags": []string{"--path", "--url", "--token", "--dry-run", "--batch-size"}},
			{"name": "import baserow", "readonly": false, "destructive": false, "idempotent": false, "dry_run": true, "scope": "local-read/baserow-write", "flags": []string{"--path", "--url", "--token", "--workspace", "--dry-run"}},
			{"name": "test fixture", "readonly": false, "destructive": false, "idempotent": true, "flags": []string{"--out", "--overwrite"}},
			{"name": "test verify", "readonly": true, "idempotent": true, "flags": []string{"--path", "--mode", "--sample-size"}},
			{"name": "test export", "readonly": false, "destructive": false, "idempotent": true, "flags": []string{"--path", "--out", "--exporter"}},
			{"name": "test full", "readonly": false, "destructive": false, "idempotent": true, "flags": []string{"--out", "--overwrite"}},
			{"name": "profile list", "readonly": true, "idempotent": true},
			{"name": "profile set", "readonly": false, "idempotent": true, "flags": []string{"--name", "--token-env", "--token-value"}},
		},
		"exporters":  exporter.Names(),
		"formats":    []string{"auto", "json", "ndjson", "table"},
		"exit_codes": map[string]int{"success": 0, "general": 1, "api": 2, "config": 3, "validation": 4, "conflict": 5},
	}
}

func filterBases(bases []airtable.Base, ids []string) []airtable.Base {
	if len(ids) == 0 {
		return bases
	}
	want := map[string]bool{}
	for _, id := range ids {
		want[strings.TrimSpace(id)] = true
	}
	var out []airtable.Base
	for _, base := range bases {
		if want[base.ID] {
			out = append(out, base)
		}
	}
	return out
}

func apiErr(err error) error {
	return &output.Error{Code: "AIRTABLE_API_ERROR", Message: err.Error(), Retryable: strings.Contains(err.Error(), "429"), ExitCode: output.ExitAPI}
}
