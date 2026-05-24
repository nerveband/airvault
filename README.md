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
airvault verify --path ./airtable-backup --format json
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

Profiles and config:

```bash
airvault profile set --name personal --token-env AIRTABLE_TOKEN
airvault config inspect --format json
```

Backup testing:

```bash
airvault test full --out /tmp/airvault-test --overwrite --format json
airvault test verify --path ./airtable-backup --format json
airvault test export --path ./airtable-backup --out /tmp/airvault-export-test --format json
```

## Agent contract

```bash
airvault schema
airvault schema --validate
airvault agent-context
airvault skill-path
```

Use `--format json` for deterministic output. Errors are structured on stderr in non-TTY or `AI_AGENT` contexts.
