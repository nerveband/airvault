# airvault

Comprehensive Airtable backup CLI for local, verifiable archives.

`airvault` backs up Airtable metadata, tables, fields, records, linked record IDs, attachments, checksums, and a gap report for Airtable surfaces that are not exposed through public APIs.

Each backup also writes `api-telemetry.json` with request counts, status-code counts, 429/rate-limit events, retry sleep time, per-base API stats, and restrictions such as auth/scope/permission failures.

## Install

```bash
curl -sSL https://raw.githubusercontent.com/nerveband/airvault/main/scripts/install.sh | bash
```

From source:

```bash
make install
```

## Usage

```bash
export AIRTABLE_TOKEN=pat...
airvault auth doctor
airvault bases list
airvault estimate --format json
airvault backup create --out ./airtable-backup --format json
airvault backup verify --path ./airtable-backup --mode exists --format json
```

Selective backup:

```bash
airvault backup create --base appXXXXXXXX --table Students --include schema,records --out ./students
airvault backup create --exclude attachments --out ./metadata-records
airvault backup create --include schema,records,attachments,comments,views --out ./full
```

Exports:

```bash
airvault export sqlite --path ./airtable-backup --out airtable.sqlite --overwrite
airvault export postgres --path ./airtable-backup --out airtable.sql --overwrite
airvault export jsonl --path ./airtable-backup --out ./jsonl --overwrite
```

Grist import:

```bash
airvault import grist --path ./airtable-backup --dry-run --format json
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

For offline use, run Grist locally with Docker and a `/persist` volume, then import into that local server. Airtable formula fields are translated best-effort into Grist formula columns and reported in `formula_translations`; any uncertain translations are marked `needs_review`.

NocoDB and Baserow imports:

```bash
airvault import nocodb --path ./airtable-backup --dry-run --format json
airvault import nocodb \
  --path ./airtable-backup \
  --url http://localhost:8080 \
  --token "$NOCODB_TOKEN" \
  --format json

airvault import baserow --path ./airtable-backup --dry-run --format json
airvault import baserow \
  --path ./airtable-backup \
  --url http://localhost:8081 \
  --token "$BASEROW_TOKEN" \
  --workspace 1 \
  --format json
```

These importers are archive-driven, so they do not require live Airtable access. They create local NocoDB/Baserow databases and tables from the backup, preserve Airtable record IDs, and store complex Airtable-only values as JSON text where there is no one-to-one target field. See `docs/nocodb-import-notes.md` and `docs/baserow-import-notes.md` for live-tested endpoint behavior and target-specific constraints.

Profiles and config:

```bash
airvault profile set --name personal --token-env AIRTABLE_TOKEN
airvault config inspect --format json
airvault config defaults --format json
airvault config set-defaults --backup-root ./airtable-backups --verify-mode exists --sample-size 25
```

With `backup_root` configured, `airvault backup create` can omit `--out`; it writes to `BACKUP_ROOT/runs/<timestamp>`.

Backup testing:

```bash
airvault test full --out /tmp/airvault-test --overwrite --format json
airvault backup verify --path ./airtable-backup --mode exists --format json
airvault backup verify --path ./airtable-backup --mode sample --sample-size 50 --format json
airvault backup verify --path ./airtable-backup --mode full --format json
airvault test verify --path ./airtable-backup --mode exists --format json
airvault test export --path ./airtable-backup --out /tmp/airvault-export-test --format json
```

## Agent contract

```bash
airvault schema
airvault schema --validate
airvault agent-context
airvault skill-path
```

Use `--format json` or `--json` for deterministic output. Errors are structured on stderr in non-TTY or `AI_AGENT` contexts.
