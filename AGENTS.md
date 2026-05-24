# airvault Agent Guide

`airvault` creates local Airtable backup archives with schema, records, attachments, checksums, and gap reports.

Auth:

```bash
export AIRTABLE_TOKEN=pat...
airvault auth doctor --format json
```

Common workflows:

```bash
airvault bases list --format json
airvault estimate --format json
airvault backup create --out ./airtable-backup --format json
airvault verify --path ./airtable-backup --format json
airvault export sqlite --path ./airtable-backup --out airtable.sqlite --overwrite --format json
airvault test full --out /tmp/airvault-test --overwrite --format json
```

Guardrails:

- Prefer `AIRTABLE_TOKEN`; avoid putting PATs in shell history with `--token`.
- Run `estimate` before a large backup.
- Run `verify` after every backup.
- Use `--include`, `--exclude`, `--base`, and `--table` for selective backups.
- Use `--deliver file:<path>` when a command should write result metadata atomically.
- Use `airvault test full` for fixture-only validation before relying on live backups.
- Prefer `backup verify --mode exists` or `--mode sample` for fast checks; use `--mode full` when every attachment hash must be rechecked.
- Rotate emergency Airtable PATs after use.
- Treat `gap-report.json` as part of the backup; Airtable interfaces, automations, permissions, and extensions are not fully portable through public APIs.
- Inspect `api-telemetry.json` after long runs; 429s, retry sleep, non-2xx restrictions, and per-base request counts are recorded there.

Discovery:

```bash
airvault schema --format json
airvault schema --validate --format json
airvault agent-context --format json
airvault skill-path --format json
```
