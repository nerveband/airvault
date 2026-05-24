package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/nerveband/airvault/internal/airtable"
	"github.com/nerveband/airvault/internal/archive"
	"github.com/nerveband/airvault/internal/config"
	"github.com/nerveband/airvault/internal/exporter"
	"github.com/nerveband/airvault/internal/output"
	"github.com/nerveband/airvault/internal/update"
	"github.com/spf13/cobra"
)

var (
	Version = "dev"
	format  = "auto"
	token   = ""
	profile = ""
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
	}
	cmd.PersistentFlags().StringVar(&format, "format", "auto", "Output format: auto, json, ndjson, table")
	cmd.PersistentFlags().StringVar(&token, "token", "", "Airtable PAT (prefer AIRTABLE_TOKEN)")
	cmd.PersistentFlags().StringVar(&profile, "profile", "", "Named profile from ~/.airvault/config.json")
	cmd.AddCommand(versionCmd(), schemaCmd(), agentContextCmd(), skillPathCmd(), authCmd(), basesCmd(), estimateCmd(), backupCmd(), verifyCmd(), exportCmd(), profileCmd(), configCmd(), jobsCmd(), feedbackCmd(), upgradeCmd())
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
		if out == "" {
			return &output.Error{Code: "VALIDATION_MISSING_OUT", Message: "--out is required", Hint: "Run: airvault backup create --out ./airtable-backup", ExitCode: output.ExitValidation}
		}
		components, err := archive.ParseComponents(include, exclude)
		if err != nil {
			return &output.Error{Code: "VALIDATION_BAD_COMPONENT", Message: err.Error(), ExitCode: output.ExitValidation}
		}
		if noAttachments {
			components.Attachments = false
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
	cmd := &cobra.Command{Use: "verify --path <backup>", Short: "Verify backup manifest and attachment checksums", RunE: func(cmd *cobra.Command, args []string) error {
		path, _ := cmd.Flags().GetString("path")
		if path == "" {
			return &output.Error{Code: "VALIDATION_MISSING_PATH", Message: "--path is required", ExitCode: output.ExitValidation}
		}
		m, err := archive.Verify(path)
		if err != nil {
			return &output.Error{Code: "VERIFY_FAILED", Message: err.Error(), ExitCode: output.ExitConflict}
		}
		return output.Write(cmd.OutOrStdout(), format, map[string]any{"ok": true, "manifest": m}, nil)
	}}
	cmd.Flags().String("path", "", "Backup archive path")
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
			"name": "airvault", "version": Version,
			"guardrails":       []string{"Use AIRTABLE_TOKEN instead of --token when possible", "Run estimate before backup", "Use backup verify after backup", "Rotate Airtable PATs after emergency backup sessions"},
			"common_workflows": []string{"airvault estimate --format json", "airvault backup create --out ./airtable-backup --format json", "airvault backup verify --path ./airtable-backup --format json"},
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
		return output.Write(cmd.OutOrStdout(), format, map[string]any{"precedence": []string{"--token", "AIRTABLE_TOKEN", "--profile token env", "--profile token"}, "config": store.Redacted()}, nil)
	}})
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
		"schema_version": "airvault-cli-schema-v1",
		"commands": []map[string]any{
			{"name": "auth doctor", "readonly": true, "idempotent": true},
			{"name": "bases list", "readonly": true, "idempotent": true},
			{"name": "estimate", "readonly": true, "idempotent": true, "flags": []string{"--base", "--table"}},
			{"name": "backup create", "readonly": false, "destructive": false, "idempotent": true, "dry_run": true, "scope": "remote-read/local-write", "flags": []string{"--out", "--base", "--table", "--include", "--exclude", "--dry-run", "--no-attachments", "--max-attachment-bytes", "--resume-job"}},
			{"name": "backup verify", "readonly": true, "idempotent": true, "flags": []string{"--path"}},
			{"name": "export", "readonly": false, "destructive": false, "idempotent": true, "flags": []string{"--path", "--out", "--deliver", "--overwrite", "--plan"}},
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
