---
name: airvault
description: Back up Airtable bases into local verifiable archives with schema, records, attachments, checksums, and gap reports.
---

# airvault

Use `airvault` when you need to preserve Airtable data before a plan downgrade, account migration, or emergency archival window.

## Workflow

1. Set `AIRTABLE_TOKEN` in the environment.
2. Run `airvault auth doctor --format json`.
3. Run `airvault estimate --format json` to get record and attachment totals.
4. Run `airvault backup create --out ./airtable-backup --format json`.
5. Run `airvault backup verify --path ./airtable-backup --mode exists --format json`.

Selective backups:

```bash
airvault backup create --base appXXXXXXXX --table Students --exclude attachments --out ./students
airvault backup create --include schema,records,attachments,comments,views --out ./full
```

Exports:

```bash
airvault export sqlite --path ./airtable-backup --out airtable.sqlite --overwrite --format json
airvault export postgres --path ./airtable-backup --out airtable.sql --overwrite --format json
airvault export jsonl --path ./airtable-backup --out ./jsonl --overwrite --format json
```

Grist import:

```bash
airvault import grist --path ./airtable-backup --dry-run --format json
airvault import grist --path ./airtable-backup --url http://localhost:8484 --workspace 3 --api-key "$GRIST_API_KEY" --include-formulas --include-attachments --report grist-migration-report.json --format json
```

Testing:

```bash
airvault test full --out /tmp/airvault-test --overwrite --format json
airvault backup verify --path ./airtable-backup --mode exists --format json
airvault backup verify --path ./airtable-backup --mode sample --sample-size 50 --format json
airvault backup verify --path ./airtable-backup --mode full --format json
airvault test verify --path ./airtable-backup --mode exists --format json
airvault test export --path ./airtable-backup --out /tmp/airvault-export-test --format json
```

Defaults:

```bash
airvault config defaults --format json
airvault config set-defaults --backup-root ./airtable-backups --verify-mode exists --sample-size 25
```

## Notes

- Attachment URLs expire, so real backup runs download files immediately.
- Linked records are preserved by Airtable record IDs.
- Formula field definitions are preserved in schema metadata when Airtable exposes them. `import grist --include-formulas` translates simple formulas into Grist formula columns and reports formulas that need review.
- Comments are backed up only when `--include comments` is set and the token has the required scope.
- Interfaces, automations, extensions, and permissions are reported in `gap-report.json` as unsupported surfaces.
- API restrictions and rate limits are reported in `api-telemetry.json`. Review it after live runs.
