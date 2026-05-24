package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/nerveband/airvault/internal/airtable"
	"github.com/nerveband/airvault/internal/archive"
	"github.com/nerveband/airvault/internal/output"
	"github.com/nerveband/airvault/internal/update"
	"github.com/spf13/cobra"
)

var (
	Version = "dev"
	format  = "auto"
	token   = ""
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
	cmd.AddCommand(versionCmd(), schemaCmd(), agentContextCmd(), authCmd(), basesCmd(), estimateCmd(), backupCmd(), verifyCmd(), upgradeCmd())
	return cmd
}

func client() (*airtable.Client, error) {
	t := token
	if t == "" {
		t = os.Getenv("AIRTABLE_TOKEN")
	}
	if t == "" {
		return nil, &output.Error{Code: "CONFIG_MISSING_TOKEN", Message: "Airtable token is required", Hint: "Set AIRTABLE_TOKEN or pass --token", ExitCode: output.ExitConfig}
	}
	return airtable.New(t), nil
}

func versionCmd() *cobra.Command {
	return &cobra.Command{Use: "version", Short: "Print version", RunE: func(cmd *cobra.Command, args []string) error {
		return output.Write(cmd.OutOrStdout(), format, map[string]string{"version": Version}, func(w io.Writer) error {
			_, err := fmt.Fprintln(w, Version)
			return err
		})
	}}
}

func authCmd() *cobra.Command {
	parent := &cobra.Command{Use: "auth", Short: "Authentication helpers"}
	parent.AddCommand(&cobra.Command{Use: "doctor", Short: "Validate Airtable token", RunE: func(cmd *cobra.Command, args []string) error {
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
	cmd.AddCommand(&cobra.Command{Use: "list", Short: "List visible bases", RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client()
		if err != nil {
			return err
		}
		bases, err := c.ListBases(context.Background())
		if err != nil {
			return apiErr(err)
		}
		return output.Write(cmd.OutOrStdout(), format, bases, func(w io.Writer) error {
			for _, b := range bases {
				fmt.Fprintf(w, "%s\t%s\t%s\n", b.ID, b.Name, b.PermissionLevel)
			}
			return nil
		})
	}})
	return cmd
}

func estimateCmd() *cobra.Command {
	var baseIDs []string
	cmd := &cobra.Command{Use: "estimate", Short: "Estimate records and attachment storage without downloading files", RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client()
		if err != nil {
			return err
		}
		bases, err := c.ListBases(context.Background())
		if err != nil {
			return apiErr(err)
		}
		filtered := filterBases(bases, baseIDs)
		m, err := archive.Estimate(context.Background(), c, filtered)
		if err != nil {
			return apiErr(err)
		}
		return output.Write(cmd.OutOrStdout(), format, m, nil)
	}}
	cmd.Flags().StringSliceVar(&baseIDs, "base", nil, "Base ID to include; repeat or comma-separate")
	return cmd
}

func backupCmd() *cobra.Command {
	var out string
	var baseIDs []string
	var dryRun bool
	var noAttachments bool
	cmd := &cobra.Command{Use: "backup", Short: "Create and verify Airtable backups"}
	create := &cobra.Command{Use: "create", Short: "Create a local backup archive", RunE: func(cmd *cobra.Command, args []string) error {
		if out == "" {
			return &output.Error{Code: "VALIDATION_MISSING_OUT", Message: "--out is required", Hint: "Run: airvault backup create --out ./airtable-backup", ExitCode: output.ExitValidation}
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
			Out: out, BaseIDs: baseIDs, DownloadAttachments: !noAttachments, ToolVersion: Version, DryRun: dryRun,
		})
		if err != nil {
			return apiErr(err)
		}
		return output.Write(cmd.OutOrStdout(), format, m, nil)
	}}
	create.Flags().StringVar(&out, "out", "", "Backup output directory")
	create.Flags().StringSliceVar(&baseIDs, "base", nil, "Base ID to include; repeat or comma-separate")
	create.Flags().BoolVar(&dryRun, "dry-run", false, "Plan backup without writing files")
	create.Flags().BoolVar(&noAttachments, "no-attachments", false, "Record attachment metadata but do not download files")
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
	return &cobra.Command{Use: "schema", Short: "Print machine-readable command schema", RunE: func(cmd *cobra.Command, args []string) error {
		return output.Write(cmd.OutOrStdout(), "json", commandSchema(), nil)
	}}
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
			{"name": "estimate", "readonly": true, "idempotent": true, "flags": []string{"--base"}},
			{"name": "backup create", "readonly": false, "destructive": false, "idempotent": true, "dry_run": true, "flags": []string{"--out", "--base", "--dry-run", "--no-attachments"}},
			{"name": "backup verify", "readonly": true, "idempotent": true, "flags": []string{"--path"}},
		},
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
