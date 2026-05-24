# airvault

Comprehensive Airtable backup CLI for local, verifiable archives.

`airvault` backs up Airtable metadata, tables, fields, records, linked record IDs, attachments, checksums, and a gap report for Airtable surfaces that are not exposed through public APIs.

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

## Agent contract

```bash
airvault schema
airvault agent-context
```

Use `--format json` for deterministic output. Errors are structured on stderr in non-TTY or `AI_AGENT` contexts.
