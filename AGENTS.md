# airvault Agent Guide

`airvault` backs up Airtable bases into local, verifiable archives. It preserves schema metadata, field IDs, records, linked record IDs, attachment files, attachment metadata, views where exposed, optional comments, checksums, API telemetry, and a gap report for Airtable-only surfaces.

## First Commands

```bash
airvault version --format json
airvault schema --format json
airvault schema --validate --format json
airvault config defaults --format json
```

## Auth

Prefer environment variables. Do not put personal access tokens in command history unless the user explicitly asks.

```bash
export AIRTABLE_TOKEN=pat...
airvault auth doctor --format json
```

Profile-based auth is supported:

```bash
airvault profile set --name personal --token-env AIRTABLE_TOKEN --format json
airvault config inspect --format json
```

Precedence is: explicit flags, `--token`, `AIRTABLE_TOKEN`, profile token env, profile token, config defaults, builtin defaults.

## Defaults

Inspect effective defaults before deciding which flags are needed:

```bash
airvault config defaults --format json
```

Useful defaults:

```bash
airvault config set-defaults \
  --backup-root ./airtable-backups \
  --include schema,records,attachments,views \
  --verify-mode exists \
  --sample-size 25 \
  --exporter jsonl,sqlite,postgres \
  --format json
```

When `backup_root` is configured, `airvault backup create` can omit `--out`; the CLI writes to `BACKUP_ROOT/runs/<timestamp>`.

## Backup Workflow

Fast preflight:

```bash
airvault bases list --count --format json
airvault estimate --format json
airvault test full --out /tmp/airvault-test --overwrite --format json
```

Default backup using configured root:

```bash
airvault backup create --format json
```

Explicit backup:

```bash
airvault backup create --out ./airtable-backup --format json
```

Selective backups:

```bash
airvault backup create --base appXXXXXXXX --table Students --exclude attachments --out ./students --format json
airvault backup create --include schema,records,attachments,comments,views --out ./full --format json
airvault backup create --include schema,records --out ./metadata-records --format json
```

## Verification

Use fast verification for routine checks:

```bash
airvault backup verify --path ./airtable-backup --mode exists --format json
```

Verification modes:

- `manifest`: manifest, gap report, jobs, telemetry presence only.
- `ledger`: count JSONL rows and compare records/comments/attachments/bytes to manifest.
- `exists`: ledger plus attachment file existence and file-size checks.
- `sample`: exists plus hashes `--sample-size` checksum entries.
- `health`: same practical check as sample.
- `full`: hashes every file in `checksums.sha256`; slow on large attachment archives.

For large backups, prefer:

```bash
airvault backup verify --path ./airtable-backup --mode exists --format json
airvault backup verify --path ./airtable-backup --mode sample --sample-size 50 --format json
```

Use full hash verification only when needed:

```bash
airvault backup verify --path ./airtable-backup --mode full --format json
```

## Exports

Available exporters:

```bash
airvault export jsonl --path ./airtable-backup --out ./jsonl --overwrite --format json
airvault export sqlite --path ./airtable-backup --out airtable.sqlite --overwrite --format json
airvault export postgres --path ./airtable-backup --out airtable.sql --overwrite --format json
```

Test all configured exporters:

```bash
airvault test export --path ./airtable-backup --out /tmp/airvault-export-test --format json
```

## Grist Import

Plan an import without writing to Grist:

```bash
airvault import grist --path ./airtable-backup --dry-run --format json
```

Import into a local or self-hosted Grist server:

```bash
airvault import grist \
  --path ./airtable-backup \
  --url http://localhost:8484 \
  --workspace 3 \
  --api-key "$GRIST_API_KEY" \
  --include-formulas \
  --include-attachments \
  --report grist-migration-report.json \
  --format json
```

For a freshly bootstrapped local Grist server, a `GRIST_COOKIE` session cookie can be used for tests when an API key has not been created yet. Do not use fake Grist servers for importer validation; run the real `gristlabs/grist` container or Grist Desktop/server target and verify imported records through the Grist API.

Formula translation is best-effort. The report lists each formula field with `translated` or `needs_review` status. Airtable functions with no direct Grist equivalent must be reviewed manually.

## Artifacts

Important files inside an archive:

- `manifest.json`: archive summary and totals.
- `api-telemetry.json`: request counts, 429s, retry sleep, status codes, per-base stats, restrictions.
- `gap-report.json`: surfaces Airtable does not expose for portable backup.
- `checksums.sha256`: attachment hashes.
- `jobs/*.json`: durable job metadata.
- `bases/*/schema.json`: Airtable schema metadata.
- `bases/*/tables/*/records.jsonl`: records keyed by Airtable field IDs.
- `bases/*/tables/*/attachments.jsonl`: attachment metadata.

## Guardrails

- Rotate emergency Airtable PATs after backup sessions.
- Run `estimate` before large live backups.
- Run `test full` before trusting a new build or environment.
- Use `--include comments` only when comments are needed; it can add many API calls.
- Review `api-telemetry.json` after long runs for rate limits or restrictions.
- Treat `gap-report.json` as part of the backup. Interfaces, automations, extensions, permissions, and some UI-only state are not fully portable.
- Prefer `--format json` or `--json` for deterministic output.
- Prefer `--deliver file:<path>` when result metadata should be written atomically.

## Current Local Convention

On this machine, the configured backup root is expected to be:

```text
/Volumes/SHAMS M1/wavedepth Dropbox/Ashraf Ali/Mac (2)/Documents/Projects/personal-stuff/ashraf-airtable-backup
```

Confirm with:

```bash
airvault config defaults --format json
```
