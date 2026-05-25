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

## Current Local Grist

The imported offline Grist instance is running in Docker/Colima:

```text
Container: airvault-grist-test
Primary LAN URL: http://192.168.1.5:8484
Fallback LAN URL: http://192.168.1.58:8484
Boot key: airvault-boot-key
Admin email: airvault@example.com
Workspace ID: 3
```

The LAN URLs are also saved in `.env`. On Grist's quick setup screen, use `http://192.168.1.5:8484` as the Base URL, click `Test URL`, click `Confirm edition`, then continue through the setup. Choose `Full Grist` unless the user asks for Community Edition; it can run locally and gives the 30-day activation path shown by Grist.

Current full import report:

```text
/Volumes/SHAMS M1/wavedepth Dropbox/Ashraf Ali/Mac (2)/Documents/Projects/personal-stuff/ashraf-airtable-backup/reports/grist-import-2026-05-24_202726.json
```

## Current NocoDB And Baserow Comparison

The user decided to stop using Grist for comparison and evaluate NocoDB and Baserow side by side. Grist was stopped after the successful import; do not restart it unless asked.

Running containers:

```text
NocoDB: airvault-nocodb
Baserow: airvault-baserow
```

LAN URLs:

```text
NocoDB: http://192.168.1.5:8080
Baserow: http://192.168.1.5:8081
```

Local comparison login:

```text
Email: airvault@example.com
Password: airvault-local-2026
```

Baserow is configured with `BASEROW_PUBLIC_URL=http://192.168.1.5:8081`, so use the `192.168.1.5` URL from browsers. `localhost` or `192.168.1.58` can hit Baserow's builder-domain fallback and return 404.

NocoDB is configured with `NC_PUBLIC_URL=http://192.168.1.5:8080`; prefer the primary LAN URL there too.

Native Airtable import notes:

- NocoDB requires a valid Airtable PAT and shared base ID/URL.
- Baserow requires a public Airtable base share link and imports one base at a time.
- Prefer archive-based imports from the local `airvault` backup if live Airtable/share links are unavailable.

Keep target-specific import learnings in:

```text
docs/nocodb-import-notes.md
docs/baserow-import-notes.md
```

Current archive import reports:

```text
NocoDB: /Volumes/SHAMS M1/wavedepth Dropbox/Ashraf Ali/Mac (2)/Documents/Projects/personal-stuff/ashraf-airtable-backup/reports/nocodb-import-typed-2026-05-24_215900.json
Baserow: /Volumes/SHAMS M1/wavedepth Dropbox/Ashraf Ali/Mac (2)/Documents/Projects/personal-stuff/ashraf-airtable-backup/reports/baserow-import-typed-2026-05-24_222800.json
```

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
