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
5. Run `airvault verify --path ./airtable-backup --format json`.

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

Testing:

```bash
airvault test full --out /tmp/airvault-test --overwrite --format json
airvault test verify --path ./airtable-backup --format json
airvault test export --path ./airtable-backup --out /tmp/airvault-export-test --format json
```

## Notes

- Attachment URLs expire, so real backup runs download files immediately.
- Linked records are preserved by Airtable record IDs.
- Formula field definitions are preserved in schema metadata when Airtable exposes them, but formulas are not translated to other tools.
- Comments are backed up only when `--include comments` is set and the token has the required scope.
- Interfaces, automations, extensions, and permissions are reported in `gap-report.json` as unsupported surfaces.
- API restrictions and rate limits are reported in `api-telemetry.json`. Review it after live runs.
